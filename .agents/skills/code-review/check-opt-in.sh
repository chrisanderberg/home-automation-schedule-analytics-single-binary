#!/bin/sh

set -eu

truthy() {
	case "${1:-}" in
		1|true|TRUE|yes|YES|on|ON)
			return 0
			;;
	esac
	return 1
}

if truthy "${AUTONOMOUS_REVIEW_OPT_IN:-}"; then
	exit 0
fi

if truthy "${ENABLE_AUTONOMOUS_REVIEWS:-}"; then
	exit 0
fi

if command -v git >/dev/null 2>&1; then
	if truthy "$(git config --get codex.autonomousReviewOptIn 2>/dev/null || true)"; then
		exit 0
	fi
	if truthy "$(git config --get codex.enableAutonomousReviews 2>/dev/null || true)"; then
		exit 0
	fi
fi

echo "CodeRabbit reviews are opt-in because code diffs are sent to an external service. Enable AUTONOMOUS_REVIEW_OPT_IN=1, ENABLE_AUTONOMOUS_REVIEWS=1, git config codex.autonomousReviewOptIn true, or git config codex.enableAutonomousReviews true."
exit 1
