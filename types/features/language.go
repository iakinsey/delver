package features

type Language struct {
	Name       string  `json:"name"`
	Confidence float64 `json:"confidence"`
}

const LangEnglish = "en"
