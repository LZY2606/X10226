package tga

import "fmt"

// validateItem converts-and-checks one raw item against its layer rules.
// Unconvertible values make the single item invalid but stay in raw form.
func validateItem(layer string, ci CommitItem) (bool, []Diagnostic) {
	diags := []Diagnostic{}
	switch layer {
	case LayerSamples:
		require(diags, ci.Raw, "time", []string{"time_s", "time", "t", "furnace_time_s"}, true)
		require(diags, ci.Raw, "temp", []string{"temp_c", "temperature", "temp", "t_c"}, true)
		require(diags, ci.Raw, "mass", []string{"mass_mg", "mass", "m", "weight"}, true)
		checkConvert(diags, ci.Raw, "time", []string{"time_s", "time", "t", "furnace_time_s"}, parseTimeS)
		checkConvert(diags, ci.Raw, "temp", []string{"temp_c", "temperature", "temp", "t_c"}, parseTempC)
		checkConvert(diags, ci.Raw, "mass", []string{"mass_mg", "mass", "m", "weight"}, parseMassMg)
	case LayerBlank:
		require(diags, ci.Raw, "time", []string{"time_s", "time", "t"}, true)
		require(diags, ci.Raw, "mass", []string{"mass_mg", "mass", "m", "blank_mass_mg", "drift_mg"}, true)
		checkConvert(diags, ci.Raw, "time", []string{"time_s", "time", "t"}, parseTimeS)
		checkConvert(diags, ci.Raw, "mass", []string{"mass_mg", "mass", "m", "blank_mass_mg", "drift_mg"}, parseMassMg)
	case LayerDrift:
		require(diags, ci.Raw, "time", []string{"time_s", "time", "t"}, true)
		require(diags, ci.Raw, "offset", []string{"offset_mg", "drift_mg", "correction_mg", "mass", "m"}, true)
		checkConvert(diags, ci.Raw, "time", []string{"time_s", "time", "t"}, parseTimeS)
		checkConvert(diags, ci.Raw, "offset", []string{"offset_mg", "drift_mg", "correction_mg", "mass", "m"}, parseMassMg)
	case LayerProgram:
		for _, f := range []struct {
			label string
			keys  []string
			parse func(string) (float64, string, bool)
		}{
			{"start", []string{"start_s", "t0", "start"}, parseTimeS},
			{"end", []string{"end_s", "t1", "end"}, parseTimeS},
			{"start_c", []string{"start_c", "temp0", "t_start"}, parseTempC},
			{"end_c", []string{"end_c", "temp1", "t_end"}, parseTempC},
		} {
			require(diags, ci.Raw, f.label, f.keys, true)
			checkConvert(diags, ci.Raw, f.label, f.keys, f.parse)
		}
	case LayerGas:
		require(diags, ci.Raw, "time", []string{"time_s", "time", "t", "valve_time_s"}, true)
		require(diags, ci.Raw, "to_gas", []string{"to_gas", "to", "gas_to", "gas"}, true)
		checkConvert(diags, ci.Raw, "time", []string{"time_s", "time", "t", "valve_time_s"}, parseTimeS)
	case LayerInitialMg:
		require(diags, ci.Raw, "mass", []string{"mass", "m0", "initial_mass", "mass_mg"}, true)
		checkConvert(diags, ci.Raw, "mass", []string{"mass", "m0", "initial_mass", "mass_mg"}, parseMassMg)
	}
	return len(diags) == 0, diags
}

func require(diags []Diagnostic, raw map[string]string, field string, keys []string, mandatory bool) {
	if _, ok := rawGet(raw, keys...); !ok && mandatory {
		diags = append(diags, Diagnostic{Code: "missing_field", Field: field,
			Message: fmt.Sprintf("缺少必需字段 %s（可接受列名 %v）", field, keys)})
	}
}

type convFn func(string) (float64, string, bool)

func checkConvert(diags []Diagnostic, raw map[string]string, field string, keys []string, fn convFn) {
	v, ok := rawGet(raw, keys...)
	if !ok {
		return
	}
	if _, _, ok := fn(v); !ok {
		diags = append(diags, Diagnostic{Code: "unconvertible_value", Field: field,
			Message: fmt.Sprintf("字段 %s 的值 %q 无法规范化，保留原始表示", field, v)})
	}
}
