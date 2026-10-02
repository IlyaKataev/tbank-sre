package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"marketplace/internal/api"
	"marketplace/internal/apierr"
)

func RequestErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)

	var fields []map[string]any
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		fields = []map[string]any{{
			"field":   typeErr.Field,
			"message": fmt.Sprintf("expected %s, got %s", typeErr.Type, typeErr.Value),
		}}
	}

	details := map[string]any{"fields": fields, "error": err.Error()}
	resp := api.ErrorResponse{
		ErrorCode: apierr.ErrValidation,
		Message:   "Ошибка валидации входных данных",
		Details:   &details,
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func ResponseErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	w.Header().Set("Content-Type", "application/json")
	status := http.StatusInternalServerError
	var ae *apierr.AppError
	if errors.As(err, &ae) {
		status = ae.HTTPStatus
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errJSON(err))
}
