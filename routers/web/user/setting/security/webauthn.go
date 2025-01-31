// Copyright 2018 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package security

import (
	"errors"
	"net/http"

	"code.gitea.io/gitea/models/auth"
	wa "code.gitea.io/gitea/modules/auth/webauthn"
	"code.gitea.io/gitea/modules/context"
	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/setting"
	"code.gitea.io/gitea/modules/web"
	"code.gitea.io/gitea/services/forms"

	"github.com/duo-labs/webauthn/protocol"
	"github.com/duo-labs/webauthn/webauthn"
)

// WebAuthnRegister initializes the webauthn registration procedure
func WebAuthnRegister(ctx *context.Context) {
	form := web.GetForm(ctx).(*forms.WebauthnRegistrationForm)
	if form.Name == "" {
		ctx.Error(http.StatusConflict)
		return
	}

	cred, err := auth.GetWebAuthnCredentialByName(ctx.User.ID, form.Name)
	if err != nil && !auth.IsErrWebAuthnCredentialNotExist(err) {
		ctx.ServerError("GetWebAuthnCredentialsByUID", err)
		return
	}
	if cred != nil {
		ctx.Error(http.StatusConflict, "Name already taken")
		return
	}

	_ = ctx.Session.Delete("registration")
	if err := ctx.Session.Set("WebauthnName", form.Name); err != nil {
		ctx.ServerError("Unable to set session key for WebauthnName", err)
		return
	}

	credentialOptions, sessionData, err := wa.WebAuthn.BeginRegistration((*wa.User)(ctx.User))
	if err != nil {
		ctx.ServerError("Unable to BeginRegistration", err)
		return
	}

	// Save the session data as marshaled JSON
	if err = ctx.Session.Set("registration", sessionData); err != nil {
		ctx.ServerError("Unable to set session", err)
		return
	}

	ctx.JSON(http.StatusOK, credentialOptions)
}

// WebauthnRegisterPost receives the response of the security key
func WebauthnRegisterPost(ctx *context.Context) {
	name, ok := ctx.Session.Get("WebauthnName").(string)
	if !ok || name == "" {
		ctx.ServerError("Get WebauthnName", errors.New("no WebauthnName"))
		return
	}

	// Load the session data
	sessionData, ok := ctx.Session.Get("registration").(*webauthn.SessionData)
	if !ok || sessionData == nil {
		ctx.ServerError("Get registration", errors.New("no registration"))
		return
	}
	defer func() {
		_ = ctx.Session.Delete("registration")
	}()

	// Verify that the challenge succeeded
	cred, err := wa.WebAuthn.FinishRegistration((*wa.User)(ctx.User), *sessionData, ctx.Req)
	if err != nil {
		if pErr, ok := err.(*protocol.Error); ok {
			log.Error("Unable to finish registration due to error: %v\nDevInfo: %s", pErr, pErr.DevInfo)
		}
		ctx.ServerError("CreateCredential", err)
		return
	}

	dbCred, err := auth.GetWebAuthnCredentialByName(ctx.User.ID, name)
	if err != nil && !auth.IsErrWebAuthnCredentialNotExist(err) {
		ctx.ServerError("GetWebAuthnCredentialsByUID", err)
		return
	}
	if dbCred != nil {
		ctx.Error(http.StatusConflict, "Name already taken")
		return
	}

	// Create the credential
	_, err = auth.CreateCredential(ctx.User.ID, name, cred)
	if err != nil {
		ctx.ServerError("CreateCredential", err)
		return
	}
	ctx.JSON(http.StatusCreated, cred)
}

// WebauthnDelete deletes an security key by id
func WebauthnDelete(ctx *context.Context) {
	form := web.GetForm(ctx).(*forms.WebauthnDeleteForm)
	if _, err := auth.DeleteCredential(form.ID, ctx.User.ID); err != nil {
		ctx.ServerError("GetWebAuthnCredentialByID", err)
		return
	}
	ctx.JSON(http.StatusOK, map[string]interface{}{
		"redirect": setting.AppSubURL + "/user/settings/security",
	})
}
