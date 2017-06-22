package sso

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/xml"
	"time"

	"github.com/beevik/etree"
	"github.com/kolide/fleet/server/kolide"
	"github.com/pkg/errors"
	gosamltypes "github.com/russellhaering/gosaml2/types"
	dsig "github.com/russellhaering/goxmldsig"
	"github.com/russellhaering/goxmldsig/etreeutils"
)

type Validator interface {
	ValidateSignature(auth kolide.Auth) (kolide.Auth, error)
	ValidateResponse(auth kolide.Auth) error
}

type validator struct {
	context  *dsig.ValidationContext
	clock    *dsig.Clock
	metadata gosamltypes.EntityDescriptor
}

func Clock(clock *dsig.Clock) func(v *validator) {
	return func(v *validator) {
		v.clock = clock
	}
}

// NewValidator is used to validate the response to an auth request.
// metadata is from the IDP.
func NewValidator(metadata string, opts ...func(v *validator)) (Validator, error) {
	var v validator

	err := xml.Unmarshal([]byte(metadata), &v.metadata)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshalling metadata")
	}
	var idpCertStore dsig.MemoryX509CertificateStore
	for _, key := range v.metadata.IDPSSODescriptor.KeyDescriptors {
		certData, err := base64.StdEncoding.DecodeString(key.KeyInfo.X509Data.X509Certificate.Data)
		if err != nil {
			return nil, errors.Wrap(err, "decoding idp x509 cert")
		}
		cert, err := x509.ParseCertificate(certData)
		if err != nil {
			return nil, errors.Wrap(err, "parsing idp x509 cert")
		}
		idpCertStore.Roots = append(idpCertStore.Roots, cert)
	}
	for _, opt := range opts {
		opt(&v)
	}
	if v.clock == nil {
		v.clock = dsig.NewRealClock()
	}
	v.context = dsig.NewDefaultValidationContext(&idpCertStore)
	v.context.Clock = v.clock
	return &v, nil
}

func (v *validator) ValidateResponse(auth kolide.Auth) error {
	info := auth.(*resp)
	// make sure response is current
	onOrAfter, err := time.Parse(time.RFC3339, info.response.Assertion.Conditions.NotOnOrAfter)
	if err != nil {
		return errors.Wrap(err, "missing timestamp from condition")
	}
	notBefore, err := time.Parse(time.RFC3339, info.response.Assertion.Conditions.NotBefore)
	if err != nil {
		return errors.Wrap(err, "missing timestamp from condition")
	}
	currentTime := v.clock.Now()
	if currentTime.After(onOrAfter) {
		return errors.New("response expired")
	}
	if currentTime.Before(notBefore) {
		return errors.New("response too early")
	}
	if auth.UserID() == "" {
		return errors.New("missing user id")
	}
	return nil
}

func (v *validator) ValidateSignature(auth kolide.Auth) (kolide.Auth, error) {
	info := auth.(*resp)
	status, err := info.status()
	if err != nil {
		return nil, errors.New("missing or malformed response")
	}
	if status != Success {
		return nil, errors.Errorf("response status %s", info.statusDescription())
	}
	decoded, err := base64.StdEncoding.DecodeString(info.rawResponse())
	if err != nil {
		return nil, errors.Wrap(err, "based64 decoding response")
	}
	doc := etree.NewDocument()
	err = doc.ReadFromBytes(decoded)
	if err != nil || doc.Root() == nil {
		return nil, errors.Wrap(err, "parsing xml response")
	}
	elt := doc.Root()
	signed, err := v.validateSignature(elt)
	if err != nil {
		return nil, errors.Wrap(err, "signing verification failed")
	}
	// We've verified that the response hasn't been tampered with at this point
	signedDoc := etree.NewDocument()
	signedDoc.SetRoot(signed)
	buffer, err := doc.WriteToBytes()
	if err != nil {
		return nil, errors.Wrap(err, "creating signed doc buffer")
	}
	var response Response
	err = xml.Unmarshal(buffer, &response)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshalling signed doc")
	}
	info.setResponse(&response)
	return info, nil
}

func (v *validator) validateSignature(elt *etree.Element) (*etree.Element, error) {
	validated, err := v.context.Validate(elt)
	if err == nil {
		// If entire doc is signed, success, we're done.
		return validated, nil
	}

	if err == dsig.ErrMissingSignature {
		// If entire document is not signed find signed assertions, remove assertions
		// that are not signed.
		err = v.validateAssertionSignature(elt)
		if err != nil {
			return nil, err
		}
		return elt, nil
	}

	return nil, err
}

func (v *validator) validateAssertionSignature(elt *etree.Element) error {
	validateAssertion := func(ctx etreeutils.NSContext, unverified *etree.Element) error {
		if unverified.Parent() != elt {
			return errors.Errorf("assertion with unexpected parent: %s", unverified.Parent().Tag)
		}
		// Remove assertions that are not signed.
		detached, err := etreeutils.NSDetatch(ctx, unverified)
		if err != nil {
			return err
		}
		signed, err := v.context.Validate(detached)
		if err != nil {
			return err
		}
		elt.RemoveChild(unverified)
		elt.AddChild(signed)
		return nil
	}
	return etreeutils.NSFindIterate(elt, "urn:oasis:names:tc:SAML:2.0:assertion", "Assertion", validateAssertion)
}