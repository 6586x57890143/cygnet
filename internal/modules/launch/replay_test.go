package launch

import (
	"testing"
	"time"

	"github.com/6586x57890143/cygnet/internal/store"
)

// SPEC experiment 6: the valve's rule replayed against a launch, a minute
// at a time, as turn runs it. Until there's a recorded launch to replay,
// the arrivals are a model: of each wave, a fifth arrive in its first
// minute (the ones cygnet adds herself, and the quick), half over the next
// four, and the rest over the ten after; a tenth never come.
func replay(line, ceiling int) (minutes, peak5 int, worst float64, idle int) {
	type wave struct {
		store.Wave
		at int
	}
	var waves []wave
	joins := make([]float64, 0, 600)
	arriving := map[int]float64{} // minute -> joins
	left := line
	idleRun := 0
	for t := 0; t < 600; t++ {
		joins = append(joins, arriving[t])
		for i := range waves {
			w := &waves[i]
			w.Arrived = int(arrivedBy(w.Admitted, t-w.at))
		}
		rate := func(end int) float64 {
			s := 0.0
			for i := max(0, end-4); i <= end; i++ {
				s += joins[i]
			}
			return s / 5
		}
		worst = max(worst, rate(t))
		var last store.Wave
		peak := 0.0
		if len(waves) > 0 {
			w := waves[len(waves)-1]
			last = w.Wave
			for end := w.at + 1; end <= t; end++ {
				peak = max(peak, rate(end))
			}
		}
		now := time.Unix(0, 0).Add(time.Duration(t) * time.Minute)
		if left > 0 && (last.ID == 0 || settled(last, rate(t), ceiling, now)) {
			size, _ := next(reading{ceiling: ceiling, last: last, joins: peak})
			size = min(size, left)
			left -= size
			w := wave{Wave: store.Wave{ID: int64(len(waves) + 1), Size: size, Admitted: size, Opened: now}, at: t}
			waves = append(waves, w)
			for m := 0; m < 15; m++ {
				arriving[t+1+m] += float64(size) * share(m)
			}
			idleRun = 0
		} else if left > 0 {
			idleRun++
			idle = max(idle, idleRun)
		}
		if left == 0 && minutes == 0 {
			minutes = t
		}
	}
	for t := 0; t+5 <= len(joins); t++ {
		s := 0.0
		for _, j := range joins[t : t+5] {
			s += j
		}
		peak5 = max(peak5, int(s))
	}
	return minutes, peak5, worst, idle
}

// share is the part of a wave that arrives in its mth minute.
func share(m int) float64 {
	switch {
	case m == 0:
		return 0.2
	case m < 5:
		return 0.5 / 4
	default:
		return 0.2 / 10
	}
}

// arrivedBy is how many of n have arrived m minutes after their wave.
func arrivedBy(n, m int) float64 {
	a := 0.0
	for i := 0; i < min(m, 15); i++ {
		a += float64(n) * share(i)
	}
	return a
}

func TestReplay(t *testing.T) {
	minutes, peak5, worst, idle := replay(2000, 10)
	t.Logf("2000 in line, ceiling 10: all let in by minute %d, at most %d joins in any 5 minutes, worst 5 minute rate %.1f a minute, longest wait between waves %d minutes", minutes, peak5, worst, idle)
	// Measured: the line through by minute 311, the 5 minute rate never
	// past 8 a minute, waves at most 7 minutes apart. Held to the ceiling,
	// a window and a bit between waves, and under 6 hours.
	if worst > 10 || minutes == 0 || minutes > 6*60 || idle > int(window/time.Minute)+2 {
		t.Fatalf("minutes %d, worst %.1f, idle %d", minutes, worst, idle)
	}
}
