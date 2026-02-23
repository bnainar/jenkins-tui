package permutation

import (
	"fmt"

	"jenkins-tui/internal/models"
)

type Input struct {
	ChoiceValues map[string][]string
	FixedValues  map[string]string
}

func Build(input Input, max int) ([]models.JobSpec, error) {
	keys := make([]string, 0, len(input.ChoiceValues))
	for k, vals := range input.ChoiceValues {
		if len(vals) == 0 {
			return nil, fmt.Errorf("choice parameter %s has no selected values", k)
		}
		keys = append(keys, k)
	}

	if len(keys) == 0 {
		spec := models.JobSpec{Params: copyMap(input.FixedValues)}
		return []models.JobSpec{spec}, nil
	}

	results := make([]models.JobSpec, 0)
	current := map[string]string{}
	var walk func(i int) error
	walk = func(i int) error {
		if i == len(keys) {
			params := copyMap(input.FixedValues)
			for k, v := range current {
				params[k] = v
			}
			results = append(results, models.JobSpec{Params: params})
			if len(results) > max {
				return fmt.Errorf("generated %d permutations, max allowed is %d", len(results), max)
			}
			return nil
		}
		key := keys[i]
		for _, value := range input.ChoiceValues[key] {
			current[key] = value
			if err := walk(i + 1); err != nil {
				return err
			}
		}
		return nil
	}

	if err := walk(0); err != nil {
		return nil, err
	}
	return results, nil
}

func copyMap(src map[string]string) map[string]string {
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
