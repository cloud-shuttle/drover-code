package convo

const calibrationEMAAlpha = 0.25

// RecordAPICalibration updates an EMA of (apiInputTokens / estimatedTokens) after
// each main agent stream completes. Both values must be from the same request:
// estimatedTokens is convo.EstimatedTokens() immediately before StreamMessage, and
// apiInputTokens is usage.InputTokens from the response.
//
// Tiny prompts are skipped (noise). Extreme ratios are skipped (tooling/metadata skew).
func (m *Manager) RecordAPICalibration(estimatedTokens, apiInputTokens int) {
	if estimatedTokens < 64 || apiInputTokens < 32 {
		return
	}
	r := float64(apiInputTokens) / float64(estimatedTokens)
	if r < 0.05 || r > 20 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.calibSamples == 0 {
		m.calibEMA = r
	} else {
		m.calibEMA = calibrationEMAAlpha*r + (1-calibrationEMAAlpha)*m.calibEMA
	}
	m.calibSamples++
	m.calibLastAPI = apiInputTokens
	m.calibLastEst = estimatedTokens
}

// CalibrationHint returns an EMA and the last sample's ratio of API input tokens
// to the pre-request heuristic. ok is false until at least one sample exists.
func (m *Manager) CalibrationHint() (emaRatio, lastRatio float64, samples int, ok bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.calibSamples == 0 {
		return 0, 0, 0, false
	}
	last := 0.0
	if m.calibLastEst > 0 {
		last = float64(m.calibLastAPI) / float64(m.calibLastEst)
	}
	return m.calibEMA, last, m.calibSamples, true
}

// ResetCalibration clears API calibration samples (e.g. after /clear).
func (m *Manager) ResetCalibration() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calibEMA = 0
	m.calibSamples = 0
	m.calibLastAPI = 0
	m.calibLastEst = 0
}
