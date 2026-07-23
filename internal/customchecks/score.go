package customchecks

// maxScore matches Scorecard's own 0-10 check scoring scale.
const maxScore = 10

// proportional mirrors Scorecard's own checker.CreateProportionalScore: an
// integer-floor proportion of success/total on the 0-10 scale, capped at
// maxScore. total is assumed non-zero; callers report inconclusive (nil)
// results themselves when there is nothing to score.
func proportional(success, total int) float64 {
	return float64(min(maxScore*success/total, maxScore))
}
