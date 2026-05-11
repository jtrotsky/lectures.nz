package model

// HostCity returns a short city code for a known host slug (e.g. "AKL", "WLG").
// Returns "???" for unrecognised slugs.
func HostCity(slug string) string {
	if c, ok := hostCities[slug]; ok {
		return c
	}
	return "???"
}

var hostCities = map[string]string{
	// Auckland
	"auckland":             "AKL",
	"aut":                  "AKL",
	"auckland-museum":      "AKL",
	"auckland-art-gallery": "AKL",
	"artgallery-nz":        "AKL",
	"artspace":             "AKL",
	"gus-fisher":           "AKL",
	"studio-one":           "AKL",
	"motat":                "AKL",
	"meetup":               "AKL",
	"ockham":               "AKL",
	"public-record":        "AKL",
	"national-library":     "AKL",
	"objectspace":          "AKL",
	// Wellington
	"victoria":             "WLG",
	"nziia":                "WLG",
	"te-papa":              "WLG",
	"rbnz":                 "WLG",
	"motu":                 "WLG",
	"nz-initiative":        "WLG",
	"gns":                  "WLG",
	"royal-society":        "WLG",
	"treasury":             "WLG",
	// Christchurch
	"canterbury":           "CHC",
	"canterbury-museum":    "CHC",
	// Dunedin
	"otago":                "DUN",
	// Hamilton
	"waikato":              "HAM",
}
