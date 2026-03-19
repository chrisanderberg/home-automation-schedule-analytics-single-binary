package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	TimeZone  string
	Location  *time.Location
	Latitude  float64
	Longitude float64
	DBPath    string
	Port      string
}

// Load reads environment configuration, applies defaults, and validates ranges.
func Load() (Config, error) {
	tz := envDefault("HAA_TIMEZONE", "UTC")
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return Config{}, fmt.Errorf("invalid timezone %q: %w", tz, err)
	}

	lat, err := requiredFloat("HAA_LATITUDE")
	if err != nil {
		return Config{}, err
	}
	if lat < -90 || lat > 90 {
		return Config{}, fmt.Errorf("HAA_LATITUDE must be in [-90, 90], got %v", lat)
	}

	lon, err := requiredFloat("HAA_LONGITUDE")
	if err != nil {
		return Config{}, err
	}
	if lon < -180 || lon > 180 {
		return Config{}, fmt.Errorf("HAA_LONGITUDE must be in [-180, 180], got %v", lon)
	}

	port := envDefault("HAA_PORT", "8080")
	portNum, err := strconv.Atoi(port)
	if err != nil {
		return Config{}, fmt.Errorf("invalid HAA_PORT=%q: %w", port, err)
	}
	if portNum < 1 || portNum > 65535 {
		return Config{}, fmt.Errorf("HAA_PORT must be in [1, 65535], got %d", portNum)
	}

	return Config{
		TimeZone:  tz,
		Location:  loc,
		Latitude:  lat,
		Longitude: lon,
		DBPath:    envDefault("HAA_DB_PATH", "data/data.sqlite"),
		Port:      port,
	}, nil
}

// envDefault returns an environment variable when set or a fallback otherwise.
func envDefault(key, def string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return def
}

// requiredFloat parses a required floating-point environment variable.
func requiredFloat(key string) (float64, error) {
	val := os.Getenv(key)
	if val == "" {
		return 0, fmt.Errorf("%s is required", key)
	}
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s=%q: %w", key, val, err)
	}
	return f, nil
}
