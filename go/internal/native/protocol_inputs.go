package native

import (
	"fmt"
	"io"
)

func decodeRecords(reader io.Reader, request analyzerRequest) ([]protocolRecord, error) {
	records := make([]protocolRecord, 0)
	err := decodeProtocol(reader, request, func(record protocolRecord) error {
		records = append(records, record)
		return nil
	})
	return records, err
}

func decodeScoreInputs(reader io.Reader, request analyzerRequest) (scoreInputs, error) {
	inputs := newScoreInputs()
	err := decodeProtocol(reader, request, inputs.add)
	return inputs, err
}

func decodeUnitScoreInputs(reader io.Reader, request analyzerRequest) (map[string]scoreInputs, error) {
	units := make(map[string]scoreInputs, len(request.Units))
	for _, unit := range request.Units {
		units[unit.ID] = newScoreInputs()
	}
	err := decodeProtocol(reader, request, func(record protocolRecord) error {
		if record.UnitID == "" {
			if record.Type == "diagnostic" {
				for id, inputs := range units {
					if err := inputs.add(record); err != nil {
						return err
					}
					units[id] = inputs
				}
			}
			return nil
		}
		inputs, exists := units[record.UnitID]
		if !exists {
			return fmt.Errorf("protocol record references unknown unit %q", record.UnitID)
		}
		if err := inputs.add(record); err != nil {
			return err
		}
		units[record.UnitID] = inputs
		return nil
	})
	return units, err
}
