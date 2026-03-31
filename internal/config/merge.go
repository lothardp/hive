package config

// Merge applies fields from other into c using upsert semantics:
//   - Scalar fields: overwrite if non-zero in other
//   - Maps (Env, Layouts): merge keys
//   - Slices (Hooks, SeedScripts): replace entirely
func (c *ProjectConfig) Merge(other *ProjectConfig) {
	if other.ComposePath != "" {
		c.ComposePath = other.ComposePath
	}
	if other.ExposePort != 0 {
		c.ExposePort = other.ExposePort
	}
	if other.SeedScripts != nil {
		c.SeedScripts = other.SeedScripts
	}
	if other.Hooks != nil {
		c.Hooks = other.Hooks
	}
	if other.PortVars != nil {
		c.PortVars = other.PortVars
	}

	// Merge maps
	if other.Env != nil {
		if c.Env == nil {
			c.Env = make(map[string]string)
		}
		for k, v := range other.Env {
			c.Env[k] = v
		}
	}
	if other.Layouts != nil {
		if c.Layouts == nil {
			c.Layouts = make(map[string]Layout)
		}
		for k, v := range other.Layouts {
			c.Layouts[k] = v
		}
	}
}
