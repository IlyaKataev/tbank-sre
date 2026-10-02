package handler

import (
	"fmt"
	"unicode/utf8"

	"marketplace/internal/apierr"
)

type fieldErr struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type validator struct {
	errs []fieldErr
}

func (v *validator) minLen(field, value string, n int) {
	if utf8.RuneCountInString(value) < n {
		v.errs = append(v.errs, fieldErr{field, fmt.Sprintf("minLength: %d", n)})
	}
}

func (v *validator) maxLen(field, value string, n int) {
	if utf8.RuneCountInString(value) > n {
		v.errs = append(v.errs, fieldErr{field, fmt.Sprintf("maxLength: %d", n)})
	}
}

func (v *validator) minFloat(field string, value, n float64) {
	if value < n {
		v.errs = append(v.errs, fieldErr{field, fmt.Sprintf("minimum: %g", n)})
	}
}

func (v *validator) minInt(field string, value, n int) {
	if value < n {
		v.errs = append(v.errs, fieldErr{field, fmt.Sprintf("minimum: %d", n)})
	}
}

func (v *validator) maxInt(field string, value, n int) {
	if value > n {
		v.errs = append(v.errs, fieldErr{field, fmt.Sprintf("maximum: %d", n)})
	}
}

func (v *validator) pattern(field, value, description string, check func(string) bool) {
	if !check(value) {
		v.errs = append(v.errs, fieldErr{field, description})
	}
}

func (v *validator) err() error {
	if len(v.errs) == 0 {
		return nil
	}
	fields := make([]map[string]any, len(v.errs))
	for i, e := range v.errs {
		fields[i] = map[string]any{"field": e.Field, "message": e.Message}
	}
	return apierr.NewWithDetails(apierr.ErrValidation,
		"Ошибка валидации входных данных",
		map[string]any{"fields": fields})
}

func isPromoCodePattern(s string) bool {
	if len(s) < 4 || len(s) > 20 {
		return false
	}
	for _, c := range s {
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}
