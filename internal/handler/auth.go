package handler

import (
	"context"
	"net/mail"

	"marketplace/internal/api"
)

func (h *Handler) RegisterUser(ctx context.Context, req api.RegisterUserRequestObject) (api.RegisterUserResponseObject, error) {
	role := "USER"
	if req.Body.Role != nil {
		role = string(*req.Body.Role)
	}
	v := &validator{}
	v.pattern("email", string(req.Body.Email), "must be a valid email address", func(value string) bool {
		address, err := mail.ParseAddress(value)
		return err == nil && address.Address == value
	})
	v.minLen("password", req.Body.Password, 8)
	v.pattern("password", req.Body.Password, "must not exceed 72 bytes", func(value string) bool {
		return len(value) <= 72
	})
	v.pattern("role", role, "public registration allows USER or SELLER", func(value string) bool {
		return value == "USER" || value == "SELLER"
	})
	if err := v.err(); err != nil {
		return api.RegisterUser400JSONResponse{ValidationErrorJSONResponse: api.ValidationErrorJSONResponse(errJSON(err))}, nil
	}

	pair, err := h.auth.Register(ctx, string(req.Body.Email), req.Body.Password, role)
	if err != nil {
		switch appErrCode(err) {
		case "USER_ALREADY_EXISTS":
			return api.RegisterUser409JSONResponse(errJSON(err)), nil
		case "VALIDATION_ERROR":
			return api.RegisterUser400JSONResponse{ValidationErrorJSONResponse: api.ValidationErrorJSONResponse(errJSON(err))}, nil
		}
		return nil, err
	}
	return api.RegisterUser201JSONResponse(toAuthResponse(pair)), nil
}

func (h *Handler) LoginUser(ctx context.Context, req api.LoginUserRequestObject) (api.LoginUserResponseObject, error) {
	pair, err := h.auth.Login(ctx, string(req.Body.Email), req.Body.Password)
	if err != nil {
		switch appErrCode(err) {
		case "INVALID_CREDENTIALS":
			return api.LoginUser401JSONResponse(errJSON(err)), nil
		case "VALIDATION_ERROR":
			return api.LoginUser400JSONResponse{ValidationErrorJSONResponse: api.ValidationErrorJSONResponse(errJSON(err))}, nil
		}
		return nil, err
	}
	return api.LoginUser200JSONResponse(toAuthResponse(pair)), nil
}

func (h *Handler) RefreshToken(ctx context.Context, req api.RefreshTokenRequestObject) (api.RefreshTokenResponseObject, error) {
	pair, err := h.auth.RefreshTokens(ctx, req.Body.RefreshToken)
	if err != nil {
		if appErrCode(err) == "REFRESH_TOKEN_INVALID" {
			return api.RefreshToken401JSONResponse(errJSON(err)), nil
		}
		return nil, err
	}
	return api.RefreshToken200JSONResponse(toAuthResponse(pair)), nil
}
