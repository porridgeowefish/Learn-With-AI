package inference

// ChargeTokens applies the rates to mutually exclusive token counts. A missing
// cache-hit count only prevents settlement if it would change the charge.
func ChargeTokens(usage map[string]int, inputRate, readRate, writeRate, outputRate int64) (int64, bool) {
	const maxTokens = 8_000_000
	input, haveInput := usage["inputTokens"]
	output, haveOutput := usage["outputTokens"]
	read, haveRead := usage["cacheReadTokens"]
	miss, haveMiss := usage["cacheMissTokens"]
	write := usage["cacheWriteTokens"]
	if !haveInput || !haveOutput || input < 0 || input > maxTokens || output < 0 || output > maxTokens ||
		write < 0 || write > input || read < 0 || read > input-write {
		return 0, false
	}

	remaining := input - write
	if haveMiss {
		if miss < 0 || miss > remaining || (haveRead && miss != remaining-read) {
			return 0, false
		}
		read = remaining - miss
	} else if !haveRead && remaining > 0 && inputRate != readRate {
		return 0, false
	}
	return int64(remaining-read)*inputRate + int64(read)*readRate +
		int64(write)*writeRate + int64(output)*outputRate, true
}
