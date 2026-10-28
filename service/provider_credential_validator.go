package service

import (
	"context"
	"crynux_as/models"
	"crynux_as/provider"
	"errors"
)

type registryCredentialValidator struct{}

func (registryCredentialValidator) ValidateCredential(ctx context.Context, sp models.ServiceProvider, secret []byte) error {
	reg := GetProviderAdapterRegistry()
	if reg == nil {
		return errors.New("provider adapters not initialized")
	}
	adapter, err := reg.Get(sp)
	if err != nil {
		return err
	}
	result, err := adapter.ValidateCredential(ctx, secret)
	if err != nil {
		return err
	}
	switch result {
	case provider.CredentialValid:
		return nil
	case provider.CredentialInvalidAuthentication:
		return ErrSharedAPIKeyInvalidSecret
	default:
		return errors.New("service provider temporarily unavailable")
	}
}

func InitSharedAPIKeyCredentialValidator() {
	SetSharedAPIKeyCredentialValidator(registryCredentialValidator{})
}
