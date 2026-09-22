package sourceestimate

import (
	"math"
	"strings"
)

// A transparent relay preserves inputs, output and the callable operation. It
// does not take ownership of a separately exposed protocol's implementation.
func gradeTransparentCall(op *operation) (call, bool) {
	if gradedSurfaceConstructor(op) {
		return call{}, false
	}
	body := normalizedPrunedBody(op)
	calls := callsIn(body)
	if len(calls) != 1 {
		return call{}, false
	}
	c := calls[0]
	if len(c.actuals) != len(op.paramNames) {
		return call{}, false
	}
	if !gradedForwardsParameters(op, c) {
		return call{}, false
	}
	localResult := gradedCallAssignedName(body, c)
	if localResult != "" && !gradedCallResultOnly(op, body, c, localResult) {
		return call{}, false
	}
	for _, tok := range body {
		switch tok.text {
		case "=", ":=":
			if localResult == "" {
				return call{}, false
			}
		case "if", "for", "while", "match", "switch", "try", "defer", "finally", "+", "-", "*", "/", "?", "&&", "||", "throw":
			return call{}, false
		}
	}
	return c, true
}

func gradedCallResultOnly(op *operation, body []token, c call, local string) bool {
	assignments := 0
	for _, tok := range body {
		if tok.text == "=" || tok.text == ":=" {
			assignments++
		}
	}
	if assignments != 1 {
		return false
	}
	end := matching(body, c.position+1, "(", ")")
	if end < 0 {
		return false
	}
	rest := body[end+1:]
	for len(rest) > 0 && rest[0].text == ";" {
		rest = rest[1:]
	}
	if len(rest) > 0 && rest[0].text == "return" {
		rest = rest[1:]
	} else if op.language != "rust" {
		return false
	}
	for len(rest) > 0 && rest[len(rest)-1].text == ";" {
		rest = rest[:len(rest)-1]
	}
	return len(rest) == 1 && rest[0].text == local
}

// An unavailable input-to-result implementation leaves two specific duties
// possible at the call site: checking the accepted input domain and producing
// the result. Use that bounded duty envelope for the estimated projection,
// separately from observed responsibility. It is one connected outcome, not
// one credit per unknown call. Already observed duties are not counted again.
// Calls on owned delegates retain their observable protocol attribution;
// unrelated calls and discarded results do not acquire this allowance.
func gradeUnresolvedReturnedDuty(roots []*operation, units []unit, byKey *operationLookup, known map[string]float64, p CalibrationProfile) float64 {
	allowance := 0.0
	for _, root := range roots {
		for _, op := range gradeOwnedOperations(root, units, byKey) {
			if connected, predicateOnly, representationOnly := gradeConnectedResult(op, units, byKey); connected {
				if representationOnly {
					transformation := known["transform"] + known["representation-transformation"] + known["invariant-transformation"]
					allowance = math.Max(allowance, math.Max(0, p.TransformationEnvelope-transformation))
					continue
				}
				if !predicateOnly {
					return gradeResultDutyEnvelopeWithCalibration(known, p)
				}
				allowance = math.Max(allowance, math.Max(0, p.ValidationEnvelope-known["validation"]))
				continue
			}
			// Composing several stages of an owned delegate also takes over
			// admission of inputs to that protocol. Missing its implementation
			// must not erase that possible validation duty.
			if known["coordination"] > 0 && known["validation"] < p.ValidationEnvelope {
				for _, candidate := range callsIn(normalizedPrunedBody(op)) {
					dot := strings.LastIndexByte(candidate.name, '.')
					if dot >= 0 && len(candidate.actuals) > 0 && gradeOwnedReceiver(op, units[op.file], candidate.name[:dot]) && resolveCall(op, candidate, units, byKey).count() == 0 {
						allowance = p.ValidationEnvelope - known["validation"]
					}
				}
			}
			c, ok := gradeUnresolvedResultCall(op)
			if !ok || len(c.actuals) == 0 || resolveCall(op, c, units, byKey).count() != 0 {
				continue
			}
			if dot := strings.LastIndexByte(c.name, '.'); dot >= 0 && gradeOwnedReceiver(op, units[op.file], c.name[:dot]) {
				continue
			}
			return gradeResultDutyEnvelopeWithCalibration(known, p)
		}
	}
	return allowance
}

func gradeResultDutyEnvelope(known map[string]float64) float64 {
	return gradeResultDutyEnvelopeWithCalibration(known, DefaultCalibration())
}

func gradeResultDutyEnvelopeWithCalibration(known map[string]float64, p CalibrationProfile) float64 {
	transformation := known["transform"] + known["representation-transformation"] + known["invariant-transformation"]
	return math.Max(0, p.ValidationEnvelope-known["validation"]) + math.Max(0, p.TransformationEnvelope-transformation)
}

func gradedForwardsParameters(op *operation, c call) bool {
	for i, arg := range c.actuals {
		for len(arg) > 2 && arg[0].text == "(" && matching(arg, 0, "(", ")") == len(arg)-1 {
			arg = arg[1 : len(arg)-1]
		}
		if len(arg) != 1 || arg[0].text != op.paramNames[i] {
			return false
		}
	}
	return true
}
