package envars

import (
	"strconv"
)

// BuildVars combines allocated ports and static env vars into a single map.
// Port vars take precedence over static env vars with the same name.
func BuildVars(ports map[string]int, staticEnv map[string]string) map[string]string {
	vars := make(map[string]string, len(ports)+len(staticEnv))
	for k, v := range staticEnv {
		vars[k] = v
	}
	for k, v := range ports {
		vars[k] = strconv.Itoa(v)
	}
	return vars
}
