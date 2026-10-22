package licensing

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/backplane-labs/backplane-lic/pkg/licensing/internal/jsonapi"
)

// Certificate block markers.
const (
	licenseFileBegin = "-----BEGIN LICENSE FILE-----"
	licenseFileEnd   = "-----END LICENSE FILE-----"
	machineFileBegin = "-----BEGIN MACHINE FILE-----"
	machineFileEnd   = "-----END MACHINE FILE-----"

	// licenseFilePrefix and machineFilePrefix are prepended to the enc
	// payload before signing.
	licenseFilePrefix = "license/"
	machineFilePrefix = "machine/"
)

// certificateEncParts is the dot-joined part count of an encrypted enc
// payload: ciphertext, IV, and auth tag.
const certificateEncParts = 3

// certificate is the {enc, sig, alg} document carried inside a file's
// base64 block.
type certificate struct {
	Enc string `json:"enc"`
	Sig string `json:"sig"`
	Alg string `json:"alg"`
}

// encodingAlgorithm is the part before '+': aes-256-gcm or base64.
func (c certificate) encodingAlgorithm() string {
	encoding, _, _ := strings.Cut(c.Alg, "+")

	return encoding
}

// signingAlgorithm is the part after '+': ed25519, rsa-*, or ecdsa-p256.
func (c certificate) signingAlgorithm() string {
	_, sign, _ := strings.Cut(c.Alg, "+")

	return sign
}

// parseCertificate strips the block markers and decodes the certificate
// JSON. begin/end bound the expected block.
func parseCertificate(block, begin, end string) (certificate, error) {
	payload := strings.TrimSpace(block)
	payload = strings.TrimPrefix(payload, begin)
	payload = strings.TrimSuffix(payload, end)
	payload = strings.TrimSpace(payload)

	// The block is line-wrapped base64; strip all whitespace.
	stripped := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\n', '\r', '\t':
			return -1
		default:
			return r
		}
	}, payload)

	raw, err := base64.StdEncoding.DecodeString(stripped)
	if err != nil {
		return certificate{}, fmt.Errorf("%w: %w", ErrCertificateInvalid, err)
	}

	cert := certificate{}
	if err := json.Unmarshal(raw, &cert); err != nil {
		return certificate{}, fmt.Errorf("%w: %w", ErrCertificateInvalid, err)
	}

	if cert.Enc == "" || cert.Sig == "" || !strings.Contains(cert.Alg, "+") {
		return certificate{}, fmt.Errorf("%w: incomplete certificate", ErrCertificateInvalid)
	}

	return cert, nil
}

// verifyCertificate checks the certificate's signature over prefix+enc.
func verifyCertificate(v Verifier, cert certificate, prefix string) error {
	sig, err := base64.StdEncoding.DecodeString(cert.Sig)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCertificateInvalid, err)
	}

	msg := []byte(prefix + cert.Enc)

	if err := v.verify(cert.signingAlgorithm(), msg, sig); err != nil {
		return fmt.Errorf("%w: %w", ErrCertificateInvalid, err)
	}

	return nil
}

// decryptCertificate decrypts an aes-256-gcm enc payload with
// SHA256(secret) as the key.
func decryptCertificate(cert certificate, secret string) ([]byte, error) {
	parts := strings.Split(cert.Enc, ".")
	if len(parts) != certificateEncParts {
		return nil, fmt.Errorf("%w: malformed enc payload", ErrCertificateInvalid)
	}

	ciphertextPart, ivPart, tagPart := parts[0], parts[1], parts[2]

	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextPart)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCertificateInvalid, err)
	}

	nonce, err := base64.StdEncoding.DecodeString(ivPart)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCertificateInvalid, err)
	}

	tag, err := base64.StdEncoding.DecodeString(tagPart)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCertificateInvalid, err)
	}

	key := sha256.Sum256([]byte(secret))

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("licensing: aes: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("licensing: gcm: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, append(ciphertext, tag...), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCertificateInvalid, err)
	}

	return plaintext, nil
}

// LicenseFile is a checked-out license file.
type LicenseFile struct {
	// ID is the license-file UUID.
	ID string `json:"-"`

	// Certificate is the PEM-like certificate block.
	Certificate string `json:"certificate"`

	// Algorithm is the certificate's encoding+signing algorithm, e.g.
	// aes-256-gcm+ed25519.
	Algorithm string `json:"algorithm"`

	// Includes lists the relationships embedded in the file.
	Includes []string `json:"includes"`

	// Issued/Expiry/TTL describe the file's validity window; Expiry is
	// null and TTL zero for perpetual files.
	Issued time.Time  `json:"issued"`
	Expiry *time.Time `json:"expiry"`
	TTL    int        `json:"ttl"`

	// LicenseID comes from the file's relationships.
	LicenseID string `json:"-"`

	clock fileClock
}

// MachineFile is a checked-out machine file.
type MachineFile struct {
	// ID is the machine-file UUID.
	ID string `json:"-"`

	// Certificate is the PEM-like certificate block.
	Certificate string `json:"certificate"`

	// Algorithm is the certificate's encoding+signing algorithm.
	Algorithm string `json:"algorithm"`

	// Includes lists the relationships embedded in the file.
	Includes []string `json:"includes"`

	// Issued/Expiry/TTL describe the file's validity window.
	Issued time.Time  `json:"issued"`
	Expiry *time.Time `json:"expiry"`
	TTL    int        `json:"ttl"`

	// MachineID/LicenseID come from the file's relationships.
	MachineID string `json:"-"`
	LicenseID string `json:"-"`

	clock fileClock
}

// ParseLicenseFile parses a license file certificate block read from
// disk.
func ParseLicenseFile(certificateBlock string) (*LicenseFile, error) {
	file := &LicenseFile{Certificate: certificateBlock, clock: fileClock{skew: SkewGuardFor(DefaultClockSkew)}}

	cert, err := file.certificate()
	if err != nil {
		return nil, err
	}

	file.Algorithm = cert.Alg

	return file, nil
}

// ParseMachineFile parses a machine file certificate block read from
// disk.
func ParseMachineFile(certificateBlock string) (*MachineFile, error) {
	file := &MachineFile{Certificate: certificateBlock, clock: fileClock{skew: SkewGuardFor(DefaultClockSkew)}}

	cert, err := file.certificate()
	if err != nil {
		return nil, err
	}

	file.Algorithm = cert.Alg

	return file, nil
}

// decodeLicenseFile decodes a license-file document.
func decodeLicenseFile(doc jsonapi.Document, file *LicenseFile) error {
	res, err := doc.One()
	if err != nil {
		return fmt.Errorf("decode license file: %w", err)
	}

	if err := jsonapi.DecodeAttributes(res, file); err != nil {
		return fmt.Errorf("decode license file: %w", err)
	}

	file.ID = res.ID
	file.LicenseID = res.RelID("license")

	return nil
}

// decodeMachineFile decodes a machine-file document.
func decodeMachineFile(doc jsonapi.Document, file *MachineFile) error {
	res, err := doc.One()
	if err != nil {
		return fmt.Errorf("decode machine file: %w", err)
	}

	if err := jsonapi.DecodeAttributes(res, file); err != nil {
		return fmt.Errorf("decode machine file: %w", err)
	}

	file.ID = res.ID
	file.MachineID = res.RelID("machine")
	file.LicenseID = res.RelID("license")

	return nil
}

// certificate parses the file's inner certificate document.
func (f *LicenseFile) certificate() (certificate, error) {
	return parseCertificate(f.Certificate, licenseFileBegin, licenseFileEnd)
}

// certificate parses the file's inner certificate document.
func (f *MachineFile) certificate() (certificate, error) {
	return parseCertificate(f.Certificate, machineFileBegin, machineFileEnd)
}

// Verify checks the file's signature with the verifier's key.
func (f *LicenseFile) Verify(v Verifier) error {
	cert, err := f.certificate()
	if err != nil {
		return err
	}

	if err := verifyCertificate(v, cert, licenseFilePrefix); err != nil {
		return fmt.Errorf("%w: %w", ErrLicenseFileNotGenuine, err)
	}

	return nil
}

// Verify checks the file's signature with the verifier's key.
func (f *MachineFile) Verify(v Verifier) error {
	cert, err := f.certificate()
	if err != nil {
		return err
	}

	if err := verifyCertificate(v, cert, machineFilePrefix); err != nil {
		return fmt.Errorf("%w: %w", ErrMachineFileNotGenuine, err)
	}

	return nil
}

// Decode decodes an unencrypted license file's dataset.
func (f *LicenseFile) Decode() (*LicenseFileDataset, error) {
	cert, err := f.certificate()
	if err != nil {
		return nil, err
	}

	if cert.encodingAlgorithm() == encodingAES256 {
		return nil, ErrFileEncrypted
	}

	payload, err := decodeBase64Payload(cert)
	if err != nil {
		return nil, err
	}

	dataset := &LicenseFileDataset{}
	if err := dataset.decode(payload, f.clock); err != nil {
		return nil, err
	}

	return dataset, nil
}

// Decrypt decrypts and decodes the license file with the license key:
// the AES key is SHA256(license key).
func (f *LicenseFile) Decrypt(licenseKey string) (*LicenseFileDataset, error) {
	cert, err := f.certificate()
	if err != nil {
		return nil, err
	}

	if cert.encodingAlgorithm() == encodingBase64 {
		return nil, ErrFileNotEncrypted
	}

	payload, err := decryptCertificate(cert, licenseKey)
	if err != nil {
		return nil, err
	}

	dataset := &LicenseFileDataset{}
	if err := dataset.decode(payload, f.clock); err != nil {
		return nil, err
	}

	return dataset, nil
}

// Decode decodes an unencrypted machine file's dataset.
func (f *MachineFile) Decode() (*MachineFileDataset, error) {
	cert, err := f.certificate()
	if err != nil {
		return nil, err
	}

	if cert.encodingAlgorithm() == encodingAES256 {
		return nil, ErrFileEncrypted
	}

	payload, err := decodeBase64Payload(cert)
	if err != nil {
		return nil, err
	}

	dataset := &MachineFileDataset{}
	if err := dataset.decode(payload, f.clock); err != nil {
		return nil, err
	}

	return dataset, nil
}

// Decrypt decrypts and decodes the machine file: the AES key is
// SHA256(license key + fingerprint).
func (f *MachineFile) Decrypt(licenseKey, fingerprint string) (*MachineFileDataset, error) {
	cert, err := f.certificate()
	if err != nil {
		return nil, err
	}

	if cert.encodingAlgorithm() == encodingBase64 {
		return nil, ErrFileNotEncrypted
	}

	payload, err := decryptCertificate(cert, licenseKey+fingerprint)
	if err != nil {
		return nil, err
	}

	dataset := &MachineFileDataset{}
	if err := dataset.decode(payload, f.clock); err != nil {
		return nil, err
	}

	return dataset, nil
}

// decodeBase64Payload decodes a plain base64-encoded dataset payload.
func decodeBase64Payload(cert certificate) ([]byte, error) {
	payload, err := base64.StdEncoding.DecodeString(cert.Enc)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCertificateInvalid, err)
	}

	return payload, nil
}

// datasetMeta is the {issued, expiry, ttl} meta object of a payload.
type datasetMeta struct {
	Issued time.Time  `json:"issued"`
	Expiry *time.Time `json:"expiry"`
	TTL    int        `json:"ttl"`
}

// LicenseFileDataset is the decrypted payload of a license file.
type LicenseFileDataset struct {
	// License is the licensed resource.
	License License `json:"-"`

	// Entitlements are the embedded entitlements.
	Entitlements []Entitlement `json:"-"`

	// Issued/Expiry/TTL describe the file's validity window.
	Issued time.Time  `json:"-"`
	Expiry *time.Time `json:"-"`
	TTL    int        `json:"-"`
}

// decode unmarshals a payload document.
func (d *LicenseFileDataset) decode(payload []byte, clock fileClock) error {
	doc, err := jsonapi.Parse(payload)
	if err != nil {
		return fmt.Errorf("decode license file payload: %w", err)
	}

	res, err := doc.One()
	if err != nil {
		return fmt.Errorf("decode license file payload: %w", err)
	}

	if err := jsonapi.DecodeAttributes(res, &d.License); err != nil {
		return fmt.Errorf("decode license file payload: %w", err)
	}

	d.License.bind(nil, res)

	meta := datasetMeta{}
	if err := doc.DecodeMeta(&meta); err != nil {
		return fmt.Errorf("decode license file meta: %w", err)
	}

	d.Issued, d.Expiry, d.TTL = meta.Issued, meta.Expiry, meta.TTL

	for _, included := range doc.Included {
		if included.Type != "entitlements" {
			continue
		}

		entitlement := Entitlement{}
		if err := decodeEntitlement(included, &entitlement); err != nil {
			return err
		}

		d.Entitlements = append(d.Entitlements, entitlement)
	}

	return clock.checkDataset(d.Issued, d.Expiry, d.TTL, ErrLicenseFileExpired)
}

// MachineFileDataset is the decrypted payload of a machine file.
type MachineFileDataset struct {
	// Machine is the machine resource.
	Machine Machine `json:"-"`

	// License is the licensed resource.
	License License `json:"-"`

	// Entitlements/Components are the embedded relationships.
	Entitlements []Entitlement `json:"-"`
	Components   []Component   `json:"-"`

	// Issued/Expiry/TTL describe the file's validity window.
	Issued time.Time  `json:"-"`
	Expiry *time.Time `json:"-"`
	TTL    int        `json:"-"`
}

// decode unmarshals a payload document.
func (d *MachineFileDataset) decode(payload []byte, clock fileClock) error {
	doc, err := jsonapi.Parse(payload)
	if err != nil {
		return fmt.Errorf("decode machine file payload: %w", err)
	}

	res, err := doc.One()
	if err != nil {
		return fmt.Errorf("decode machine file payload: %w", err)
	}

	if err := jsonapi.DecodeAttributes(res, &d.Machine); err != nil {
		return fmt.Errorf("decode machine file payload: %w", err)
	}

	d.Machine.bind(nil, res)

	meta := datasetMeta{}
	if err := doc.DecodeMeta(&meta); err != nil {
		return fmt.Errorf("decode machine file meta: %w", err)
	}

	d.Issued, d.Expiry, d.TTL = meta.Issued, meta.Expiry, meta.TTL

	for _, included := range doc.Included {
		if err := d.decodeIncluded(included); err != nil {
			return err
		}
	}

	return clock.checkDataset(d.Issued, d.Expiry, d.TTL, ErrMachineFileExpired)
}

// decodeIncluded decodes one included resource by its type.
func (d *MachineFileDataset) decodeIncluded(res jsonapi.Resource) error {
	switch res.Type {
	case "licenses":
		if err := jsonapi.DecodeAttributes(res, &d.License); err != nil {
			return fmt.Errorf("decode included license: %w", err)
		}

		d.License.bind(nil, res)

		return nil
	case "entitlements":
		entitlement := Entitlement{}
		if err := decodeEntitlement(res, &entitlement); err != nil {
			return err
		}

		d.Entitlements = append(d.Entitlements, entitlement)
	case "components":
		component := Component{}
		if err := decodeComponent(res, &component); err != nil {
			return err
		}

		d.Components = append(d.Components, component)
	default:
		return nil
	}

	return nil
}
