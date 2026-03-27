#!/usr/bin/env python3
import argparse
import json
import math
import sys
import urllib.parse
import urllib.request


POWER_TOLERANCE = 1e-10
POWER_ITERATIONS = 512


def load_json(path):
    with open(path, "r", encoding="utf-8") as fh:
        return json.load(fh)


def fetch_json(url):
    with urllib.request.urlopen(url) as resp:
        return json.load(resp)


def parse_args():
    parser = argparse.ArgumentParser(description="Independent analytics reference checker")
    parser.add_argument("--raw-file")
    parser.add_argument("--report-file")
    parser.add_argument("--base-url")
    parser.add_argument("--control-id")
    parser.add_argument("--model-id")
    parser.add_argument("--quarter")
    parser.add_argument("--clock", default="utc")
    parser.add_argument("--smoothing")
    parser.add_argument("--kernel-radius")
    parser.add_argument("--kernel-sigma")
    parser.add_argument("--holding-damping-millis")
    parser.add_argument("--transition-damping-count")
    parser.add_argument("--self-test", action="store_true")
    return parser.parse_args()


def get_clock_payload(payload, requested_clock):
    if "clock" in payload:
        return payload["clock"]
    for clock in payload.get("clocks", []):
        if clock["clockSlug"] == requested_clock:
            return clock
    raise ValueError(f"clock {requested_clock!r} not found")


def normalize(values):
    total = sum(values)
    if total <= 0:
        if not values:
            return []
        share = 1.0 / float(len(values))
        return [share for _ in values]
    return [value / total for value in values]


def smooth_none(series):
    return list(series)


def smooth_gaussian(series, radius, sigma):
    if radius == 0:
        return list(series)
    weights = []
    total_weight = 0.0
    for offset in range(-radius, radius + 1):
        weight = math.exp(-0.5 * ((float(offset) / sigma) ** 2))
        weights.append(weight)
        total_weight += weight
    result = []
    for idx in range(len(series)):
        smoothed = 0.0
        for offset in range(-radius, radius + 1):
            source = (idx + offset + len(series)) % len(series)
            smoothed += series[source] * weights[offset + radius]
        result.append(smoothed / total_weight)
    return result


def smooth(series, parameters):
    kind = parameters["smoothing"]["kind"]
    if kind == "none":
        return smooth_none(series)
    if kind == "gaussian":
        return smooth_gaussian(
            series,
            int(parameters["smoothing"]["kernelRadius"]),
            float(parameters["smoothing"]["kernelSigma"]),
        )
    raise ValueError(f"unsupported smoothing kind {kind!r}")


def zero_matrix(n):
    return [[0.0 for _ in range(n)] for _ in range(n)]


def infer_preference(smoothed_holding, smoothed_transitions, parameters):
    fallback = normalize(smoothed_holding)
    total_holding = sum(smoothed_holding)
    total_transitions = sum(sum(row) for row in smoothed_transitions)
    if total_holding == 0 and total_transitions == 0:
        return fallback, zero_matrix(len(smoothed_holding)), True

    rates = zero_matrix(len(smoothed_holding))
    lam = 0.0
    holding_damping = float(parameters["damping"]["holdingMillis"])
    transition_damping = float(parameters["damping"]["transitionCount"])
    for from_state in range(len(smoothed_holding)):
        denominator = smoothed_holding[from_state] + holding_damping
        if denominator <= 0:
            return fallback, rates, True
        row_sum = 0.0
        for to_state in range(len(smoothed_holding)):
            if from_state == to_state:
                continue
            rate = (smoothed_transitions[from_state][to_state] + transition_damping) / denominator
            if rate < 0 or math.isnan(rate) or math.isinf(rate):
                return fallback, rates, True
            rates[from_state][to_state] = rate
            row_sum += rate
        lam = max(lam, row_sum)
    if lam <= 0:
        return fallback, rates, True

    current = list(fallback)
    for _ in range(POWER_ITERATIONS):
        next_values = [0.0 for _ in current]
        for from_state, weight in enumerate(current):
            row_sum = 0.0
            for to_state in range(len(current)):
                if from_state == to_state:
                    continue
                p = rates[from_state][to_state] / lam
                row_sum += p
                next_values[to_state] += weight * p
            next_values[from_state] += weight * (1.0 - row_sum)
        next_values = normalize(next_values)
        max_delta = max(abs(a - b) for a, b in zip(next_values, current))
        current = next_values
        if max_delta < POWER_TOLERANCE:
            return current, rates, False
    return fallback, rates, True


def expected_from_raw(raw_payload, report_payload):
    clock_slug = get_clock_payload(report_payload, report_payload["clock"]["clockSlug"])["clockSlug"] if "clock" in report_payload else report_payload["clocks"][0]["clockSlug"]
    raw_clock = get_clock_payload(raw_payload, clock_slug)
    report_clock = get_clock_payload(report_payload, clock_slug)
    parameters = report_payload["parameters"]

    holding_series = [list(map(float, series["buckets"])) for series in raw_clock["holdingMillis"]]
    transition_series = {}
    for series in raw_clock["transitionCounts"]:
        transition_series[(series["fromState"], series["toState"])] = list(map(float, series["buckets"]))

    occupancy = [[] for _ in holding_series]
    preference = [[] for _ in holding_series]
    smoothed_holding = [smooth(series, parameters) for series in holding_series]
    smoothed_transition = {}
    rates_by_transition = {}
    for key, series in transition_series.items():
        smoothed_transition[key] = smooth(series, parameters)
        rates_by_transition[key] = [0.0 for _ in series]

    fallback_buckets = 0
    num_states = len(holding_series)
    num_buckets = len(holding_series[0]) if holding_series else 0
    for bucket in range(num_buckets):
        raw_occupancy = [holding_series[state][bucket] for state in range(num_states)]
        bucket_holding = [smoothed_holding[state][bucket] for state in range(num_states)]
        bucket_transitions = zero_matrix(num_states)
        for (from_state, to_state), series in smoothed_transition.items():
            bucket_transitions[from_state][to_state] = series[bucket]
        occupancy_dist = normalize(raw_occupancy)
        preference_dist, rates, fallback = infer_preference(bucket_holding, bucket_transitions, parameters)
        if fallback:
            fallback_buckets += 1
        for state in range(num_states):
            occupancy[state].append(occupancy_dist[state])
            preference[state].append(preference_dist[state])
            for to_state in range(num_states):
                if state == to_state:
                    continue
                rates_by_transition[(state, to_state)][bucket] = rates[state][to_state]

    return {
        "occupancy": occupancy,
        "preference": preference,
        "smoothed_holding": smoothed_holding,
        "smoothed_transition": smoothed_transition,
        "rates": rates_by_transition,
        "fallback_buckets": fallback_buckets,
        "clock": report_clock,
    }


def require_close(label, got, want, tolerance=1e-6):
    if abs(got - want) > tolerance:
        raise AssertionError(f"{label}: got {got} want {want} tolerance {tolerance}")


def compare_series(expected, actual, label, tolerance=1e-6):
    if len(expected) != len(actual):
        raise AssertionError(f"{label}: length mismatch {len(expected)} != {len(actual)}")
    for idx, (got, want) in enumerate(zip(actual, expected)):
        require_close(f"{label}[{idx}]", got, want, tolerance)


def compare_report(raw_payload, report_payload):
    expected = expected_from_raw(raw_payload, report_payload)
    report_clock = expected["clock"]

    for state_idx, series in enumerate(report_clock["occupancySeries"]):
        compare_series(expected["occupancy"][state_idx], series["buckets"], f"occupancy state {state_idx}")
    for state_idx, series in enumerate(report_clock["preferenceSeries"]):
        compare_series(expected["preference"][state_idx], series["buckets"], f"preference state {state_idx}")

    diagnostics = report_clock.get("diagnostics")
    if diagnostics is not None:
        if diagnostics["fallbackBuckets"] != expected["fallback_buckets"]:
            raise AssertionError(
                f"fallbackBuckets: got {diagnostics['fallbackBuckets']} want {expected['fallback_buckets']}"
            )

    intermediates = report_clock.get("intermediates") or {}
    if "smoothedHoldingMillis" in intermediates:
        for state_idx, series in enumerate(intermediates["smoothedHoldingMillis"]):
            compare_series(expected["smoothed_holding"][state_idx], series["buckets"], f"smoothed holding state {state_idx}")
    if "smoothedTransitionCounts" in intermediates:
        for series in intermediates["smoothedTransitionCounts"]:
            key = (series["fromState"], series["toState"])
            compare_series(expected["smoothed_transition"][key], series["buckets"], f"smoothed transition {key}")
    if "transitionRates" in intermediates:
        for series in intermediates["transitionRates"]:
            key = (series["fromState"], series["toState"])
            compare_series(expected["rates"][key], series["buckets"], f"transition rate {key}")


def build_remote_payloads(args):
    if not args.base_url or not args.control_id or not args.model_id or args.quarter is None:
        raise SystemExit("--base-url requires --control-id, --model-id, and --quarter")
    report_query = {
        "controlId": args.control_id,
        "modelId": args.model_id,
        "quarter": str(args.quarter),
        "clock": args.clock,
    }
    for key, value in (
        ("smoothing", args.smoothing),
        ("kernelRadius", args.kernel_radius),
        ("kernelSigma", args.kernel_sigma),
        ("holdingDampingMillis", args.holding_damping_millis),
        ("transitionDampingCount", args.transition_damping_count),
    ):
        if value is not None:
            report_query[key] = value
    raw_query = {
        "controlId": args.control_id,
        "modelId": args.model_id,
        "quarter": str(args.quarter),
        "clock": args.clock,
    }
    raw_url = f"{args.base_url.rstrip('/')}/api/v1/analytics/raw?{urllib.parse.urlencode(raw_query, doseq=True)}"
    report_url = f"{args.base_url.rstrip('/')}/api/v1/analytics/report?{urllib.parse.urlencode(report_query, doseq=True)}"
    return fetch_json(raw_url), fetch_json(report_url)


def self_test():
    raw_payload = {
        "controlId": "mode",
        "modelId": "weekday",
        "quarterIndex": 12,
        "quarterLabel": "1973 Q1",
        "clock": {
            "clockSlug": "utc",
            "holdingMillis": [
                {"state": 0, "label": "off", "buckets": [300000.0, 0.0]},
                {"state": 1, "label": "on", "buckets": [0.0, 300000.0]},
            ],
            "transitionCounts": [
                {"fromState": 0, "toState": 1, "buckets": [1.0, 0.0]},
                {"fromState": 1, "toState": 0, "buckets": [0.0, 0.0]},
            ],
        },
    }
    report_payload = {
        "parameters": {
            "smoothing": {"kind": "none", "kernelRadius": 0, "kernelSigma": 0},
            "damping": {"holdingMillis": 0, "transitionCount": 0},
        },
        "clock": {
            "clockSlug": "utc",
            "occupancySeries": [],
            "preferenceSeries": [],
            "diagnostics": {"fallbackBuckets": 0},
            "intermediates": {"transitionRates": []},
        },
    }
    expected = expected_from_raw(raw_payload, report_payload)
    report_payload["clock"]["occupancySeries"] = [
        {"state": idx, "buckets": buckets} for idx, buckets in enumerate(expected["occupancy"])
    ]
    report_payload["clock"]["preferenceSeries"] = [
        {"state": idx, "buckets": buckets} for idx, buckets in enumerate(expected["preference"])
    ]
    report_payload["clock"]["diagnostics"]["fallbackBuckets"] = expected["fallback_buckets"]
    report_payload["clock"]["intermediates"]["transitionRates"] = [
        {"fromState": from_state, "toState": to_state, "buckets": buckets}
        for (from_state, to_state), buckets in sorted(expected["rates"].items())
        if from_state != to_state
    ]
    compare_report(raw_payload, report_payload)


def main():
    args = parse_args()
    if args.self_test:
        self_test()
        print("reference harness self-test passed")
        return 0

    if args.raw_file and args.report_file:
        raw_payload = load_json(args.raw_file)
        report_payload = load_json(args.report_file)
    elif args.base_url:
        raw_payload, report_payload = build_remote_payloads(args)
    else:
        raise SystemExit("provide --raw-file/--report-file, --base-url, or --self-test")

    compare_report(raw_payload, report_payload)
    print("analytics reference check passed")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except AssertionError as exc:
        print(f"reference check failed: {exc}", file=sys.stderr)
        raise SystemExit(1)
