package licensing

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/backplane-labs/backplane-lic/pkg/licensing/internal/jsonapi"
)

// Encoding algorithms of certificate files.
const (
	encodingAES256 = "aes-256-gcm"
	encodingBase64 = "base64"
)

// Signing algorithms accepted in checkout options, in Keygen's wire
// form.
const (
	// SignEd25519 is the default signing algorithm.
	SignEd25519 = algEd25519
	// SignRSAPSSSHA256 signs with RSA-PSS-SHA256.
	SignRSAPSSSHA256 = algRSAPSSSHA256
	// SignRSASHA256 signs with RSA-SHA256.
	SignRSASHA256 = algRSASHA256
	// SignECDSAP256 signs with ECDSA P-256.
	SignECDSAP256 = algECDSAP256
)

// CheckoutOptions configure a license or machine file checkout.
type CheckoutOptions struct {
	// Encrypt switches the certificate payload between AES-256-GCM and
	// plain base64. Default: true.
	Encrypt bool

	// Sign is the signing algorithm. Default: ed25519.
	Sign string

	// TTL is the file's lifetime; zero means the server default
	// (one month).
	TTL time.Duration

	// Include lists related resources embedded in the file.
	Include []string
}

// CheckoutOption configures a checkout.
type CheckoutOption func(*CheckoutOptions)

// apply folds the options into o over the defaults.
func (o *CheckoutOptions) apply(defaults CheckoutOptions, options ...CheckoutOption) {
	*o = defaults

	for _, option := range options {
		option(o)
	}
}

// query renders the checkout parameters: encrypt, algorithm, ttl, and
// include ride the query string on the POST checkout endpoint.
func (o CheckoutOptions) query() url.Values {
	encoding := encodingBase64
	if o.Encrypt {
		encoding = encodingAES256
	}

	sign := o.Sign
	if sign == "" {
		sign = SignEd25519
	}

	query := url.Values{}
	query.Set("encrypt", strconv.FormatBool(o.Encrypt))
	query.Set("algorithm", encoding+"+"+sign)

	if ttl := int(o.TTL.Seconds()); ttl > 0 {
		query.Set("ttl", strconv.Itoa(ttl))
	}

	if len(o.Include) > 0 {
		query.Set("include", strings.Join(o.Include, ","))
	}

	return query
}

// CheckoutTTL sets the file's time-to-live (minimum one hour).
func CheckoutTTL(ttl time.Duration) CheckoutOption {
	return func(o *CheckoutOptions) { o.TTL = ttl }
}

// CheckoutInclude embeds the given relationships in the file, e.g.
// "entitlements" or "license", "license.entitlements".
func CheckoutInclude(include ...string) CheckoutOption {
	return func(o *CheckoutOptions) { o.Include = append(o.Include, include...) }
}

// CheckoutEncrypt toggles payload encryption.
func CheckoutEncrypt(encrypt bool) CheckoutOption {
	return func(o *CheckoutOptions) { o.Encrypt = encrypt }
}

// CheckoutSign selects the signing algorithm.
func CheckoutSign(algorithm string) CheckoutOption {
	return func(o *CheckoutOptions) { o.Sign = algorithm }
}

// Checkout checks out an encrypted license file for the license. With
// Config.PublicKey set the certificate is verified before it is
// returned. Decrypt the file with the license key.
func (l *License) Checkout(ctx context.Context, options ...CheckoutOption) (*LicenseFile, error) {
	if l.client == nil {
		return nil, fmt.Errorf("%w: license", ErrUnbound)
	}

	opts := CheckoutOptions{}
	opts.apply(CheckoutOptions{Encrypt: true, Include: []string{"entitlements"}}, options...)

	file := &LicenseFile{clock: fileClock{skew: l.client.skew}}

	err := l.client.do(ctx, "POST", "licenses/"+l.ID+"/actions/check-out", opts.query(),
		jsonapi.Body{}, func(doc jsonapi.Document) error {
			return decodeLicenseFile(doc, file)
		})
	if err != nil {
		return nil, err
	}

	if l.client.verifier.configured() {
		if err := file.Verify(l.client.verifier); err != nil {
			return nil, err
		}
	}

	return file, nil
}

// Checkout checks out an encrypted machine file for the machine.
// Decrypt the file with the license key concatenated with the machine's
// fingerprint.
func (m *Machine) Checkout(ctx context.Context, options ...CheckoutOption) (*MachineFile, error) {
	if m.client == nil {
		return nil, fmt.Errorf("%w: machine", ErrUnbound)
	}

	opts := CheckoutOptions{}
	opts.apply(
		CheckoutOptions{Encrypt: true, Include: []string{"license", "license.entitlements"}},
		options...,
	)

	file := &MachineFile{clock: fileClock{skew: m.client.skew}}

	err := m.client.do(ctx, "POST", "machines/"+m.ID+"/actions/check-out", opts.query(),
		jsonapi.Body{}, func(doc jsonapi.Document) error {
			return decodeMachineFile(doc, file)
		})
	if err != nil {
		return nil, err
	}

	if m.client.verifier.configured() {
		if err := file.Verify(m.client.verifier); err != nil {
			return nil, err
		}
	}

	return file, nil
}
