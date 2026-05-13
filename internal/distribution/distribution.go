package distribution

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

// Sampler defines an interface for sampling a duration.
type Sampler interface {
	Sample() time.Duration
}

// NewFromXML creates a new Sampler from XML attributes.
// This acts as a factory and keeps the parsing logic contained within this package.
func NewFromXML(distType, milliseconds, mean, stdev, min, max string) (Sampler, error) {
	if distType == "" {
		distType = "fixed"
	}

	switch strings.ToLower(distType) {
	case "fixed":
		ms, err := parseDurationMs(milliseconds)
		if err != nil {
			return nil, fmt.Errorf("fixed distribution: invalid milliseconds: %w", err)
		}
		return &Fixed{Value: ms}, nil

	case "uniform":
		if strings.TrimSpace(min) == "" {
			return nil, fmt.Errorf("uniform distribution: min attribute is required")
		}
		if strings.TrimSpace(max) == "" {
			return nil, fmt.Errorf("uniform distribution: max attribute is required")
		}
		minFloat, err := parseFloat(min)
		if err != nil {
			return nil, fmt.Errorf("uniform distribution: invalid min: %w", err)
		}
		maxFloat, err := parseFloat(max)
		if err != nil {
			return nil, fmt.Errorf("uniform distribution: invalid max: %w", err)
		}
		if minFloat < 0 {
			return nil, fmt.Errorf("uniform distribution: min must be non-negative")
		}
		if maxFloat < 0 {
			return nil, fmt.Errorf("uniform distribution: max must be non-negative")
		}
		minMs := time.Duration(minFloat * float64(time.Millisecond))
		maxMs := time.Duration(maxFloat * float64(time.Millisecond))
		if minMs > maxMs {
			return nil, fmt.Errorf("uniform distribution: min (%v) cannot be greater than max (%v)", minMs, maxMs)
		}
		return &Uniform{Min: minMs, Max: maxMs}, nil

	case "normal":
		if strings.TrimSpace(mean) == "" {
			return nil, fmt.Errorf("normal distribution: mean attribute is required")
		}
		if strings.TrimSpace(stdev) == "" {
			return nil, fmt.Errorf("normal distribution: stdev attribute is required")
		}
		meanFloat, err := parseFloat(mean)
		if err != nil {
			return nil, fmt.Errorf("normal distribution: invalid mean: %w", err)
		}
		stdevFloat, err := parseFloat(stdev)
		if err != nil {
			return nil, fmt.Errorf("normal distribution: invalid stdev: %w", err)
		}
		if meanFloat < 0 {
			return nil, fmt.Errorf("normal distribution: mean must be non-negative")
		}
		if stdevFloat < 0 {
			return nil, fmt.Errorf("normal distribution: stdev must be non-negative")
		}
		return &Normal{
			Mean:  time.Duration(meanFloat * float64(time.Millisecond)),
			Stdev: time.Duration(stdevFloat * float64(time.Millisecond)),
		}, nil

	default:
		return nil, fmt.Errorf("unsupported distribution type: %q", distType)
	}
}

// Fixed distribution returns a constant value.
type Fixed struct {
	Value time.Duration
}

// Sample returns the fixed duration.
func (f *Fixed) Sample() time.Duration {
	return f.Value
}

// Uniform distribution returns a random value within a range.
type Uniform struct {
	Min, Max time.Duration
}

// Sample returns a random duration between Min and Max.
func (u *Uniform) Sample() time.Duration {
	if u.Min >= u.Max {
		return u.Min
	}
	// rand.Int63n works with int64, so we convert durations to nanoseconds.
	minNs := u.Min.Nanoseconds()
	maxNs := u.Max.Nanoseconds()
	delta := maxNs - minNs
	return time.Duration(minNs + rand.Int63n(delta))
}

// Normal distribution (Gaussian).
type Normal struct {
	Mean, Stdev time.Duration
}

// Sample returns a normally distributed random duration.
// The result is capped at 0 to prevent negative durations.
func (n *Normal) Sample() time.Duration {
	// Use rand.NormFloat64 which gives a normal distribution with mean 0 and stdev 1.
	// We then scale and shift it.
	sample := rand.NormFloat64()*float64(n.Stdev) + float64(n.Mean)
	if sample < 0 {
		return 0
	}
	return time.Duration(sample)
}

func parseDurationMs(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	ms, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}
	if ms < 0 {
		return 0, fmt.Errorf("duration cannot be negative: %d", ms)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

func parseFloat(value string) (float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	return strconv.ParseFloat(value, 64)
}
