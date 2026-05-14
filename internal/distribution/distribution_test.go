package distribution

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

func TestNewFromXML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		distType    string
		ms          string
		mean        string
		stdev       string
		min         string
		max         string
		wantErr     string
		assertValue func(t *testing.T, sampler Sampler)
	}{
		{
			name:     "default fixed distribution",
			distType: "",
			ms:       "150",
			assertValue: func(t *testing.T, sampler Sampler) {
				t.Helper()
				got, ok := sampler.(*Fixed)
				if !ok {
					t.Fatalf("expected *Fixed, got %T", sampler)
				}
				if got.Value != 150*time.Millisecond {
					t.Fatalf("expected 150ms, got %v", got.Value)
				}
			},
		},
		{
			name:     "uniform distribution",
			distType: "uniform",
			min:      "25",
			max:      "75",
			assertValue: func(t *testing.T, sampler Sampler) {
				t.Helper()
				got, ok := sampler.(*Uniform)
				if !ok {
					t.Fatalf("expected *Uniform, got %T", sampler)
				}
				if got.Min != 25*time.Millisecond || got.Max != 75*time.Millisecond {
					t.Fatalf("unexpected bounds: min=%v max=%v", got.Min, got.Max)
				}
			},
		},
		{
			name:     "uniform distribution supports fractional milliseconds",
			distType: "uniform",
			min:      "10.5",
			max:      "20.75",
			assertValue: func(t *testing.T, sampler Sampler) {
				t.Helper()
				got, ok := sampler.(*Uniform)
				if !ok {
					t.Fatalf("expected *Uniform, got %T", sampler)
				}
				if got.Min != 10500000*time.Nanosecond {
					t.Fatalf("expected min 10.5ms, got %v", got.Min)
				}
				if got.Max != 20750000*time.Nanosecond {
					t.Fatalf("expected max 20.75ms, got %v", got.Max)
				}
			},
		},
		{
			name:     "normal distribution supports fractional milliseconds",
			distType: "normal",
			mean:     "100.5",
			stdev:    "7.25",
			assertValue: func(t *testing.T, sampler Sampler) {
				t.Helper()
				got, ok := sampler.(*Normal)
				if !ok {
					t.Fatalf("expected *Normal, got %T", sampler)
				}
				if got.Mean != 100500000*time.Nanosecond {
					t.Fatalf("expected mean 100.5ms, got %v", got.Mean)
				}
				if got.Stdev != 7250000*time.Nanosecond {
					t.Fatalf("expected stdev 7.25ms, got %v", got.Stdev)
				}
			},
		},
		{
			name:     "unsupported distribution",
			distType: "triangle",
			wantErr:  "unsupported distribution type",
		},
		{
			name:     "fixed invalid milliseconds",
			distType: "fixed",
			ms:       "abc",
			wantErr:  "fixed distribution: invalid milliseconds",
		},
		{
			name:     "uniform min greater than max",
			distType: "uniform",
			min:      "20",
			max:      "10",
			wantErr:  "uniform distribution: min",
		},
		{
			name:     "uniform negative min",
			distType: "uniform",
			min:      "-5",
			max:      "10",
			wantErr:  "uniform distribution: min must be non-negative",
		},
		{
			name:     "uniform negative max",
			distType: "uniform",
			min:      "5",
			max:      "-10",
			wantErr:  "uniform distribution: max must be non-negative",
		},
		{
			name:     "normal negative mean",
			distType: "normal",
			mean:     "-1",
			stdev:    "1",
			wantErr:  "normal distribution: mean must be non-negative",
		},
		{
			name:     "normal negative stdev",
			distType: "normal",
			mean:     "1",
			stdev:    "-1",
			wantErr:  "normal distribution: stdev must be non-negative",
		},
		{
			name:     "normal both mean and stdev zero is rejected",
			distType: "normal",
			mean:     "0",
			stdev:    "0",
			wantErr:  "normal distribution: mean and stdev cannot both be zero",
		},
		{
			name:     "normal empty mean is rejected",
			distType: "normal",
			mean:     "",
			stdev:    "10",
			wantErr:  "normal distribution: mean attribute is required",
		},
		{
			name:     "normal empty stdev is rejected",
			distType: "normal",
			mean:     "100",
			stdev:    "",
			wantErr:  "normal distribution: stdev attribute is required",
		},
		{
			name:     "uniform empty min is rejected",
			distType: "uniform",
			min:      "",
			max:      "100",
			wantErr:  "uniform distribution: min attribute is required",
		},
		{
			name:     "uniform empty max is rejected",
			distType: "uniform",
			min:      "10",
			max:      "",
			wantErr:  "uniform distribution: max attribute is required",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sampler, err := NewFromXML(tt.distType, tt.ms, tt.mean, tt.stdev, tt.min, tt.max)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.assertValue != nil {
				tt.assertValue(t, sampler)
			}
		})
	}
}

func TestFixedSample(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewSource(42))
	s := &Fixed{Value: 42 * time.Millisecond}
	if got := s.Sample(rng); got != 42*time.Millisecond {
		t.Fatalf("expected 42ms, got %v", got)
	}
}

func TestUniformSample(t *testing.T) {
	t.Parallel()

	t.Run("equal bounds", func(t *testing.T) {
		t.Parallel()
		rng := rand.New(rand.NewSource(42))
		s := &Uniform{Min: 30 * time.Millisecond, Max: 30 * time.Millisecond}
		if got := s.Sample(rng); got != 30*time.Millisecond {
			t.Fatalf("expected 30ms, got %v", got)
		}
	})

	t.Run("sample stays within range", func(t *testing.T) {
		t.Parallel()
		rng := rand.New(rand.NewSource(42))
		s := &Uniform{Min: 10 * time.Millisecond, Max: 20 * time.Millisecond}
		for i := 0; i < 100; i++ {
			got := s.Sample(rng)
			if got < 10*time.Millisecond || got >= 20*time.Millisecond {
				t.Fatalf("sample out of range: %v", got)
			}
		}
	})
}

func TestNormalSample(t *testing.T) {
	t.Parallel()

	t.Run("returns mean when stdev is zero", func(t *testing.T) {
		t.Parallel()
		rng := rand.New(rand.NewSource(42))
		s := &Normal{Mean: 12 * time.Millisecond, Stdev: 0}
		if got := s.Sample(rng); got != 12*time.Millisecond {
			t.Fatalf("expected 12ms, got %v", got)
		}
	})
}
