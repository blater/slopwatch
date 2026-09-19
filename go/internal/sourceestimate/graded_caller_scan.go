package sourceestimate

func gradedCallerScanJava(units []unit, byKey map[string][]*operation, resolver gradedCallerResolver, consumers []consumerRecord, controlled map[string]bool, transitions map[string]map[string]map[string]bool, record func(fieldRef, *operation)) []consumerRecord {
	for _, u := range units {
		if normalizeLanguage(u.file.Language, u.file.Path) != "java" {
			continue
		}
		for _, op := range u.ops {
			consumers = append(consumers, gradedCallerScanOperation(u, op, units, byKey, resolver, controlled, transitions, record)...)
		}
	}
	return consumers
}
