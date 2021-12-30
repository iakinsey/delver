package util

func DedupeStrSlice(slice []string) (deduped []string) {
	if len(slice) == 0 {
		return deduped
	}

	keys := make(map[string]bool)

	for _, entity := range slice {
		if _, value := keys[entity]; !value {
			keys[entity] = true
			deduped = append(deduped, entity)
		}
	}
	return deduped
}
