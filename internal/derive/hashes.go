package derive

import "tgaline/internal/domain"

func inputsHash(docs []*domain.Document, p domain.Parameters, tag string) string {
	key := tag + "|algo=" + p.AlgorithmVersion + "|fp=" + ParamsFingerprint(paramsMap(p))
	for _, d := range docs {
		if d == nil {
			key += "|doc=<nil>"
			continue
		}
		key += "|doc=" + d.ID + "#v" + fkv(float64(d.Version))
	}
	return short(key)
}
