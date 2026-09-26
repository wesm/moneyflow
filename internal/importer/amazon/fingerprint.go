package amazon

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strconv"

	"github.com/wesm/moneyflow/internal/domain"
)

// FingerprintPair separates stable pairing identity from provider-fact change detection.
type FingerprintPair struct {
	Identity string
	Full     string
}

// ASINLessKey derives a stable, non-reversible key for a row without an ASIN.
func ASINLessKey(productName string) (string, error) {
	key, err := domain.CollisionKey(productName)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(key))
	return "amazon:asinless:" + hex.EncodeToString(digest[:]), nil
}

// Fingerprints derives the versioned identity and full provider-fact digests for a row.
func Fingerprints(row Row) (FingerprintPair, error) {
	asin := row.ASIN
	if asin == "" {
		asin = row.ASINLessKey
	}
	if row.OrderID == "" || asin == "" || row.ProductName == "" || row.OrderDate.Year() == 0 {
		return FingerprintPair{}, errors.New("fingerprint Amazon row: identity field is empty")
	}
	identityFields := []string{
		"amazon-identity-v1", row.OrderID, asin, row.OrderDate.String(), row.ProductName,
		strconv.FormatInt(row.Quantity, 10), strconv.FormatInt(row.AmountMinor, 10),
		string(row.Currency), strconv.Itoa(int(row.Scale)),
	}
	identity := canonicalDigest(identityFields)
	unitPrice := ""
	if row.UnitPriceMinor != nil {
		unitPrice = strconv.FormatInt(*row.UnitPriceMinor, 10)
	}
	fullFields := append([]string{"amazon-full-v1"}, identityFields[1:]...)
	fullFields = append(fullFields, unitPrice, row.OrderStatus, row.ShipmentStatus)
	return FingerprintPair{Identity: identity, Full: canonicalDigest(fullFields)}, nil
}

func canonicalDigest(fields []string) string {
	hash := sha256.New()
	var size [8]byte
	for _, field := range fields {
		binary.BigEndian.PutUint64(size[:], uint64(len(field)))
		_, _ = hash.Write(size[:])
		_, _ = hash.Write([]byte(field))
	}
	return hex.EncodeToString(hash.Sum(nil))
}
