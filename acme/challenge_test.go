package acme

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/smallstep/assert"
	"github.com/smallstep/certificates/db"
	"github.com/smallstep/cli/jose"
	"github.com/smallstep/nosql"
	"github.com/smallstep/nosql/database"
)

var testOps = ChallengeOptions{
	AccountID: "accID",
	AuthzID:   "authzID",
	Identifier: Identifier{
		Type:  "", // will get set correctly depending on the "new.." method.
		Value: "zap.internal",
	},
}

func newDNSCh() (challenge, error) {
	mockdb := &db.MockNoSQLDB{
		MCmpAndSwap: func(bucket, key, old, newval []byte) ([]byte, bool, error) {
			return []byte("foo"), true, nil
		},
	}
	return newDNS01Challenge(mockdb, testOps)
}

func newTLSALPNCh() (challenge, error) {
	mockdb := &db.MockNoSQLDB{
		MCmpAndSwap: func(bucket, key, old, newval []byte) ([]byte, bool, error) {
			return []byte("foo"), true, nil
		},
	}
	return newTLSALPN01Challenge(mockdb, testOps)
}

func newHTTPCh() (challenge, error) {
	mockdb := &db.MockNoSQLDB{
		MCmpAndSwap: func(bucket, key, old, newval []byte) ([]byte, bool, error) {
			return []byte("foo"), true, nil
		},
	}
	return newHTTP01Challenge(mockdb, testOps)
}

func TestNewHTTP01Challenge(t *testing.T) {
	ops := ChallengeOptions{
		AccountID: "accID",
		AuthzID:   "authzID",
		Identifier: Identifier{
			Type:  "http",
			Value: "zap.internal",
		},
	}
	type test struct {
		ops ChallengeOptions
		db  nosql.DB
		err *Error
	}
	tests := map[string]test{
		"fail/store-error": {
			ops: ops,
			db: &db.MockNoSQLDB{
				MCmpAndSwap: func(bucket, key, old, newval []byte) ([]byte, bool, error) {
					return nil, false, errors.New("force")
				},
			},
			err: ServerInternalErr(errors.New("error saving acme challenge: force")),
		},
		"ok": {
			ops: ops,
			db: &db.MockNoSQLDB{
				MCmpAndSwap: func(bucket, key, old, newval []byte) ([]byte, bool, error) {
					return []byte("foo"), true, nil
				},
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			ch, err := newHTTP01Challenge(tc.db, tc.ops)
			if err != nil {
				if assert.NotNil(t, tc.err) {
					ae, ok := err.(*Error)
					assert.True(t, ok)
					assert.HasPrefix(t, ae.Error(), tc.err.Error())
					assert.Equals(t, ae.StatusCode(), tc.err.StatusCode())
					assert.Equals(t, ae.Type, tc.err.Type)
				}
			} else {
				if assert.Nil(t, tc.err) {
					assert.Equals(t, ch.getAccountID(), ops.AccountID)
					assert.Equals(t, ch.getAuthzID(), ops.AuthzID)
					assert.Equals(t, ch.getType(), "http-01")
					assert.Equals(t, ch.getValue(), "zap.internal")
					assert.Equals(t, ch.getStatus(), StatusPending)

					assert.True(t, ch.getValidated().IsZero())
					assert.True(t, ch.getCreated().Before(time.Now().UTC().Add(time.Minute)))
					assert.True(t, ch.getCreated().After(time.Now().UTC().Add(-1*time.Minute)))

					assert.True(t, ch.getID() != "")
					assert.True(t, ch.getToken() != "")
				}
			}
		})
	}
}

func TestNewTLSALPN01Challenge(t *testing.T) {
	ops := ChallengeOptions{
		AccountID: "accID",
		AuthzID:   "authzID",
		Identifier: Identifier{
			Type:  "http",
			Value: "zap.internal",
		},
	}
	type test struct {
		ops ChallengeOptions
		db  nosql.DB
		err *Error
	}
	tests := map[string]test{
		"fail/store-error": {
			ops: ops,
			db: &db.MockNoSQLDB{
				MCmpAndSwap: func(bucket, key, old, newval []byte) ([]byte, bool, error) {
					return nil, false, errors.New("force")
				},
			},
			err: ServerInternalErr(errors.New("error saving acme challenge: force")),
		},
		"ok": {
			ops: ops,
			db: &db.MockNoSQLDB{
				MCmpAndSwap: func(bucket, key, old, newval []byte) ([]byte, bool, error) {
					return []byte("foo"), true, nil
				},
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			ch, err := newTLSALPN01Challenge(tc.db, tc.ops)
			if err != nil {
				if assert.NotNil(t, tc.err) {
					ae, ok := err.(*Error)
					assert.True(t, ok)
					assert.HasPrefix(t, ae.Error(), tc.err.Error())
					assert.Equals(t, ae.StatusCode(), tc.err.StatusCode())
					assert.Equals(t, ae.Type, tc.err.Type)
				}
			} else {
				if assert.Nil(t, tc.err) {
					assert.Equals(t, ch.getAccountID(), ops.AccountID)
					assert.Equals(t, ch.getAuthzID(), ops.AuthzID)
					assert.Equals(t, ch.getType(), "tls-alpn-01")
					assert.Equals(t, ch.getValue(), "zap.internal")
					assert.Equals(t, ch.getStatus(), StatusPending)

					assert.True(t, ch.getValidated().IsZero())
					assert.True(t, ch.getCreated().Before(time.Now().UTC().Add(time.Minute)))
					assert.True(t, ch.getCreated().After(time.Now().UTC().Add(-1*time.Minute)))

					assert.True(t, ch.getID() != "")
					assert.True(t, ch.getToken() != "")
				}
			}
		})
	}
}

func TestNewDNS01Challenge(t *testing.T) {
	ops := ChallengeOptions{
		AccountID: "accID",
		AuthzID:   "authzID",
		Identifier: Identifier{
			Type:  "dns",
			Value: "zap.internal",
		},
	}
	type test struct {
		ops ChallengeOptions
		db  nosql.DB
		err *Error
	}
	tests := map[string]test{
		"fail/store-error": {
			ops: ops,
			db: &db.MockNoSQLDB{
				MCmpAndSwap: func(bucket, key, old, newval []byte) ([]byte, bool, error) {
					return nil, false, errors.New("force")
				},
			},
			err: ServerInternalErr(errors.New("error saving acme challenge: force")),
		},
		"ok": {
			ops: ops,
			db: &db.MockNoSQLDB{
				MCmpAndSwap: func(bucket, key, old, newval []byte) ([]byte, bool, error) {
					return []byte("foo"), true, nil
				},
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			ch, err := newDNS01Challenge(tc.db, tc.ops)
			if err != nil {
				if assert.NotNil(t, tc.err) {
					ae, ok := err.(*Error)
					assert.True(t, ok)
					assert.HasPrefix(t, ae.Error(), tc.err.Error())
					assert.Equals(t, ae.StatusCode(), tc.err.StatusCode())
					assert.Equals(t, ae.Type, tc.err.Type)
				}
			} else {
				if assert.Nil(t, tc.err) {
					assert.Equals(t, ch.getAccountID(), ops.AccountID)
					assert.Equals(t, ch.getAuthzID(), ops.AuthzID)
					assert.Equals(t, ch.getType(), "dns-01")
					assert.Equals(t, ch.getValue(), "zap.internal")
					assert.Equals(t, ch.getStatus(), StatusPending)

					assert.True(t, ch.getValidated().IsZero())
					assert.True(t, ch.getCreated().Before(time.Now().UTC().Add(time.Minute)))
					assert.True(t, ch.getCreated().After(time.Now().UTC().Add(-1*time.Minute)))

					assert.True(t, ch.getID() != "")
					assert.True(t, ch.getToken() != "")
				}
			}
		})
	}
}

func TestChallengeToACME_Valid(t *testing.T) {
	dir := newDirectory("ca.smallstep.com", "acme")

	n := clock.Now()

	fns := []func() (challenge, error){
		newDNSCh,
		newHTTPCh,
		newTLSALPNCh,
	}
	chs := make([]challenge, 3)

	for i, f := range fns {
		ch, err := f()
		assert.FatalError(t, err)
		b := ch.clone()
		b.Validated = n
		chs[i] = b.morph()
	}

	prov := newProv()
	provName := url.PathEscape(prov.GetName())
	baseURL := &url.URL{Scheme: "https", Host: "test.ca.smallstep.com"}
	ctx := context.WithValue(context.Background(), ProvisionerContextKey, prov)
	ctx = context.WithValue(ctx, BaseURLContextKey, baseURL)
	tests := map[string]challenge{
		"dns":      chs[0],
		"http":     chs[1],
		"tls-alpn": chs[2],
	}

	for name, ch := range tests {
		t.Run(name, func(t *testing.T) {
			ach, err := ch.toACME(ctx, dir)
			assert.FatalError(t, err)

			assert.Equals(t, ach.Type, ch.getType())
			assert.Equals(t, ach.Status, ch.getStatus())
			assert.Equals(t, ach.Token, ch.getToken())
			assert.Equals(t, ach.URL,
				fmt.Sprintf("%s/acme/%s/challenge/%s",
					baseURL.String(), provName, ch.getID()))
			assert.Equals(t, ach.ID, ch.getID())
			assert.Equals(t, ach.AuthzID, ch.getAuthzID())

			v, err := time.Parse(time.RFC3339, ach.Validated)
			assert.FatalError(t, err)
			assert.Equals(t, v, ch.getValidated())

			assert.Equals(t, ach.RetryAfter, "")
		})
	}
}

func TestChallengeToACME_Retry(t *testing.T) {
	dir := newDirectory("example.com", "acme")

	n := clock.Now()

	fns := []func() (challenge, error){
		newDNSCh,
		newHTTPCh,
		newTLSALPNCh,
	}
	states := []*Retry{
		nil,
		{NextAttempt: n.Format(time.RFC3339)},
	}
	chs := make([]challenge, len(fns)*len(states))

	for i, s := range states {
		for j, f := range fns {
			ch, err := f()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = "processing"
			b.Retry = s
			chs[j+i*len(fns)] = b.morph()
		}
	}

	prov := newProv()
	provName := url.PathEscape(prov.GetName())
	baseURL := &url.URL{Scheme: "https", Host: "example.com"}
	ctx := context.WithValue(context.Background(), ProvisionerContextKey, prov)
	ctx = context.WithValue(ctx, BaseURLContextKey, baseURL)

	tests := map[string]challenge{
		"dns_no-retry":      chs[0+0*len(fns)],
		"http_no-retry":     chs[1+0*len(fns)],
		"tls-alpn_no-retry": chs[2+0*len(fns)],
		"dns_retry":         chs[0+1*len(fns)],
		"http_retry":        chs[1+1*len(fns)],
		"tls_alpn_retry":    chs[2+1*len(fns)],
	}
	for name, ch := range tests {
		t.Run(name, func(t *testing.T) {
			ach, err := ch.toACME(ctx, dir)
			assert.FatalError(t, err)

			assert.Equals(t, ach.Type, ch.getType())
			assert.Equals(t, ach.Status, ch.getStatus())
			assert.Equals(t, ach.Token, ch.getToken())
			assert.Equals(t, ach.URL,
				fmt.Sprintf("%s/acme/%s/challenge/%s",
					baseURL.String(), provName, ch.getID()))
			assert.Equals(t, ach.ID, ch.getID())
			assert.Equals(t, ach.AuthzID, ch.getAuthzID())

			assert.Equals(t, ach.Validated, "")
			if ch.getRetry() != nil {
				assert.Equals(t, ach.RetryAfter, ch.getRetry().NextAttempt)
			} else {
				assert.Equals(t, ach.RetryAfter, "")
			}
		})
	}
}

func TestChallengeSave(t *testing.T) {
	type test struct {
		ch  challenge
		old challenge
		db  nosql.DB
		err *Error
	}
	tests := map[string]func(t *testing.T) test{
		"fail/old-nil/swap-error": func(t *testing.T) test {
			httpCh, err := newHTTPCh()
			assert.FatalError(t, err)
			return test{
				ch:  httpCh,
				old: nil,
				db: &db.MockNoSQLDB{
					MCmpAndSwap: func(bucket, key, old, newval []byte) ([]byte, bool, error) {
						return nil, false, errors.New("force")
					},
				},
				err: ServerInternalErr(errors.New("error saving acme challenge: force")),
			}
		},
		"fail/old-nil/swap-false": func(t *testing.T) test {
			httpCh, err := newHTTPCh()
			assert.FatalError(t, err)
			return test{
				ch:  httpCh,
				old: nil,
				db: &db.MockNoSQLDB{
					MCmpAndSwap: func(bucket, key, old, newval []byte) ([]byte, bool, error) {
						return []byte("foo"), false, nil
					},
				},
				err: ServerInternalErr(errors.New("error saving acme challenge; acme challenge has changed since last read")),
			}
		},
		"ok/old-nil": func(t *testing.T) test {
			httpCh, err := newHTTPCh()
			assert.FatalError(t, err)
			b, err := json.Marshal(httpCh)
			assert.FatalError(t, err)
			return test{
				ch:  httpCh,
				old: nil,
				db: &db.MockNoSQLDB{
					MCmpAndSwap: func(bucket, key, old, newval []byte) ([]byte, bool, error) {
						assert.Equals(t, old, nil)
						assert.Equals(t, b, newval)
						assert.Equals(t, bucket, challengeTable)
						assert.Equals(t, []byte(httpCh.getID()), key)
						return []byte("foo"), true, nil
					},
				},
			}
		},
		"ok/old-not-nil": func(t *testing.T) test {
			oldHTTPCh, err := newHTTPCh()
			assert.FatalError(t, err)
			httpCh, err := newHTTPCh()
			assert.FatalError(t, err)

			oldb, err := json.Marshal(oldHTTPCh)
			assert.FatalError(t, err)
			b, err := json.Marshal(httpCh)
			assert.FatalError(t, err)
			return test{
				ch:  httpCh,
				old: oldHTTPCh,
				db: &db.MockNoSQLDB{
					MCmpAndSwap: func(bucket, key, old, newval []byte) ([]byte, bool, error) {
						assert.Equals(t, old, oldb)
						assert.Equals(t, b, newval)
						assert.Equals(t, bucket, challengeTable)
						assert.Equals(t, []byte(httpCh.getID()), key)
						return []byte("foo"), true, nil
					},
				},
			}
		},
	}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			tc := run(t)
			if err := tc.ch.save(tc.db, tc.old); err != nil {
				if assert.NotNil(t, tc.err) {
					ae, ok := err.(*Error)
					assert.True(t, ok)
					assert.HasPrefix(t, ae.Error(), tc.err.Error())
					assert.Equals(t, ae.StatusCode(), tc.err.StatusCode())
					assert.Equals(t, ae.Type, tc.err.Type)
				}
			} else {
				assert.Nil(t, tc.err)
			}
		})
	}
}

func TestChallengeClone(t *testing.T) {
	ch, err := newHTTPCh()
	assert.FatalError(t, err)

	clone := ch.clone()

	assert.Equals(t, clone.getID(), ch.getID())
	assert.Equals(t, clone.getAccountID(), ch.getAccountID())
	assert.Equals(t, clone.getAuthzID(), ch.getAuthzID())
	assert.Equals(t, clone.getStatus(), ch.getStatus())
	assert.Equals(t, clone.getToken(), ch.getToken())
	assert.Equals(t, clone.getCreated(), ch.getCreated())
	assert.Equals(t, clone.getValidated(), ch.getValidated())

	clone.Status = StatusValid

	assert.NotEquals(t, clone.getStatus(), ch.getStatus())
}

func TestChallengeUnmarshal(t *testing.T) {
	type test struct {
		ch  challenge
		chb []byte
		err *Error
	}
	tests := map[string]func(t *testing.T) test{
		"fail/nil": func(t *testing.T) test {
			return test{
				chb: nil,
				err: ServerInternalErr(errors.New("error unmarshaling challenge type: unexpected end of JSON input")),
			}
		},
		"fail/unexpected-type-http": func(t *testing.T) test {
			httpCh, err := newHTTPCh()
			assert.FatalError(t, err)
			_httpCh, ok := httpCh.(*http01Challenge)
			assert.Fatal(t, ok)
			_httpCh.baseChallenge.Type = "foo"
			b, err := json.Marshal(httpCh)
			assert.FatalError(t, err)
			return test{
				chb: b,
				err: ServerInternalErr(errors.New("unexpected challenge type foo")),
			}
		},
		"fail/unexpected-type-alpn": func(t *testing.T) test {
			tlsALPNCh, err := newTLSALPNCh()
			assert.FatalError(t, err)
			_tlsALPNCh, ok := tlsALPNCh.(*tlsALPN01Challenge)
			assert.Fatal(t, ok)
			_tlsALPNCh.baseChallenge.Type = "foo"
			b, err := json.Marshal(tlsALPNCh)
			assert.FatalError(t, err)
			return test{
				chb: b,
				err: ServerInternalErr(errors.New("unexpected challenge type foo")),
			}
		},
		"fail/unexpected-type-dns": func(t *testing.T) test {
			dnsCh, err := newDNSCh()
			assert.FatalError(t, err)
			_dnsCh, ok := dnsCh.(*dns01Challenge)
			assert.Fatal(t, ok)
			_dnsCh.baseChallenge.Type = "foo"
			b, err := json.Marshal(dnsCh)
			assert.FatalError(t, err)
			return test{
				chb: b,
				err: ServerInternalErr(errors.New("unexpected challenge type foo")),
			}
		},
		"ok/dns": func(t *testing.T) test {
			dnsCh, err := newDNSCh()
			assert.FatalError(t, err)
			b, err := json.Marshal(dnsCh)
			assert.FatalError(t, err)
			return test{
				ch:  dnsCh,
				chb: b,
			}
		},
		"ok/http": func(t *testing.T) test {
			httpCh, err := newHTTPCh()
			assert.FatalError(t, err)
			b, err := json.Marshal(httpCh)
			assert.FatalError(t, err)
			return test{
				ch:  httpCh,
				chb: b,
			}
		},
		"ok/alpn": func(t *testing.T) test {
			tlsALPNCh, err := newTLSALPNCh()
			assert.FatalError(t, err)
			b, err := json.Marshal(tlsALPNCh)
			assert.FatalError(t, err)
			return test{
				ch:  tlsALPNCh,
				chb: b,
			}
		},
		"ok/err": func(t *testing.T) test {
			httpCh, err := newHTTPCh()
			assert.FatalError(t, err)
			_httpCh, ok := httpCh.(*http01Challenge)
			assert.Fatal(t, ok)
			_httpCh.baseChallenge.Error = ServerInternalErr(errors.New("force")).ToACME()
			b, err := json.Marshal(httpCh)
			assert.FatalError(t, err)
			return test{
				ch:  httpCh,
				chb: b,
			}
		},
	}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			tc := run(t)
			if ch, err := unmarshalChallenge(tc.chb); err != nil {
				if assert.NotNil(t, tc.err) {
					ae, ok := err.(*Error)
					assert.True(t, ok)
					assert.HasPrefix(t, ae.Error(), tc.err.Error())
					assert.Equals(t, ae.StatusCode(), tc.err.StatusCode())
					assert.Equals(t, ae.Type, tc.err.Type)
				}
			} else {
				if assert.Nil(t, tc.err) {
					assert.Equals(t, tc.ch.getID(), ch.getID())
					assert.Equals(t, tc.ch.getAccountID(), ch.getAccountID())
					assert.Equals(t, tc.ch.getAuthzID(), ch.getAuthzID())
					assert.Equals(t, tc.ch.getStatus(), ch.getStatus())
					assert.Equals(t, tc.ch.getToken(), ch.getToken())
					assert.Equals(t, tc.ch.getCreated(), ch.getCreated())
					assert.Equals(t, tc.ch.getValidated(), ch.getValidated())
				}
			}
		})
	}
}
func TestGetChallenge(t *testing.T) {
	type test struct {
		id  string
		db  nosql.DB
		ch  challenge
		err *Error
	}
	tests := map[string]func(t *testing.T) test{
		"fail/not-found": func(t *testing.T) test {
			dnsCh, err := newDNSCh()
			assert.FatalError(t, err)
			return test{
				ch: dnsCh,
				id: dnsCh.getID(),
				db: &db.MockNoSQLDB{
					MGet: func(bucket, key []byte) ([]byte, error) {
						return nil, database.ErrNotFound
					},
				},
				err: MalformedErr(errors.Errorf("challenge %s not found: not found", dnsCh.getID())),
			}
		},
		"fail/db-error": func(t *testing.T) test {
			dnsCh, err := newDNSCh()
			assert.FatalError(t, err)
			return test{
				ch: dnsCh,
				id: dnsCh.getID(),
				db: &db.MockNoSQLDB{
					MGet: func(bucket, key []byte) ([]byte, error) {
						return nil, errors.New("force")
					},
				},
				err: ServerInternalErr(errors.Errorf("error loading challenge %s: force", dnsCh.getID())),
			}
		},
		"fail/unmarshal-error": func(t *testing.T) test {
			dnsCh, err := newDNSCh()
			assert.FatalError(t, err)
			_dnsCh, ok := dnsCh.(*dns01Challenge)
			assert.Fatal(t, ok)
			_dnsCh.baseChallenge.Type = "foo"
			b, err := json.Marshal(dnsCh)
			assert.FatalError(t, err)
			return test{
				ch: dnsCh,
				id: dnsCh.getID(),
				db: &db.MockNoSQLDB{
					MGet: func(bucket, key []byte) ([]byte, error) {
						assert.Equals(t, bucket, challengeTable)
						assert.Equals(t, key, []byte(dnsCh.getID()))
						return b, nil
					},
				},
				err: ServerInternalErr(errors.New("unexpected challenge type foo")),
			}
		},
		"ok": func(t *testing.T) test {
			dnsCh, err := newDNSCh()
			assert.FatalError(t, err)
			b, err := json.Marshal(dnsCh)
			assert.FatalError(t, err)
			return test{
				ch: dnsCh,
				id: dnsCh.getID(),
				db: &db.MockNoSQLDB{
					MGet: func(bucket, key []byte) ([]byte, error) {
						assert.Equals(t, bucket, challengeTable)
						assert.Equals(t, key, []byte(dnsCh.getID()))
						return b, nil
					},
				},
			}
		},
	}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			tc := run(t)
			if ch, err := getChallenge(tc.db, tc.id); err != nil {
				if assert.NotNil(t, tc.err) {
					ae, ok := err.(*Error)
					assert.True(t, ok)
					assert.HasPrefix(t, ae.Error(), tc.err.Error())
					assert.Equals(t, ae.StatusCode(), tc.err.StatusCode())
					assert.Equals(t, ae.Type, tc.err.Type)
				}
			} else {
				if assert.Nil(t, tc.err) {
					assert.Equals(t, tc.ch.getID(), ch.getID())
					assert.Equals(t, tc.ch.getAccountID(), ch.getAccountID())
					assert.Equals(t, tc.ch.getAuthzID(), ch.getAuthzID())
					assert.Equals(t, tc.ch.getStatus(), ch.getStatus())
					assert.Equals(t, tc.ch.getToken(), ch.getToken())
					assert.Equals(t, tc.ch.getCreated(), ch.getCreated())
					assert.Equals(t, tc.ch.getValidated(), ch.getValidated())
				}
			}
		})
	}
}

func TestKeyAuthorization(t *testing.T) {
	type test struct {
		token string
		jwk   *jose.JSONWebKey
		exp   string
		err   *Error
	}
	tests := map[string]func(t *testing.T) test{
		"fail/jwk-thumbprint-error": func(t *testing.T) test {
			jwk, err := jose.GenerateJWK("EC", "P-256", "ES256", "sig", "", 0)
			assert.FatalError(t, err)
			jwk.Key = "foo"
			return test{
				token: "1234",
				jwk:   jwk,
				err:   ServerInternalErr(errors.Errorf("error generating JWK thumbprint: square/go-jose: unknown key type 'string'")),
			}
		},
		"ok": func(t *testing.T) test {
			token := "1234"
			jwk, err := jose.GenerateJWK("EC", "P-256", "ES256", "sig", "", 0)
			assert.FatalError(t, err)
			thumbprint, err := jwk.Thumbprint(crypto.SHA256)
			assert.FatalError(t, err)
			encPrint := base64.RawURLEncoding.EncodeToString(thumbprint)
			return test{
				token: token,
				jwk:   jwk,
				exp:   fmt.Sprintf("%s.%s", token, encPrint),
			}
		},
	}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			tc := run(t)
			if ka, err := KeyAuthorization(tc.token, tc.jwk); err != nil {
				if assert.NotNil(t, tc.err) {
					ae, ok := err.(*Error)
					assert.True(t, ok)
					assert.HasPrefix(t, ae.Error(), tc.err.Error())
					assert.Equals(t, ae.StatusCode(), tc.err.StatusCode())
					assert.Equals(t, ae.Type, tc.err.Type)
				}
			} else {
				if assert.Nil(t, tc.err) {
					assert.Equals(t, tc.exp, ka)
				}
			}
		})
	}
}

type errReader int

func (errReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("force")
}
func (errReader) Close() error {
	return nil
}

func TestHTTP01Validate(t *testing.T) {
	type test struct {
		vo  validateOptions
		ch  challenge
		res challenge
		jwk *jose.JSONWebKey
		err *Error
	}
	tests := map[string]func(t *testing.T) test{

		"valid/status-noop": func(t *testing.T) test {
			ch, err := newHTTPCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusValid
			ch = b.morph()
			return test{
				ch:  ch,
				res: ch,
			}
		},

		"invalid/status-noop": func(t *testing.T) test {
			ch, err := newHTTPCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusInvalid
			ch = b.morph()
			return test{
				ch:  ch,
				res: ch,
			}
		},

		"error/status-pending": func(t *testing.T) test {
			ch, err := newHTTPCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusPending
			e := errors.New("pending challenges must first be moved to the processing state")
			return test{
				ch:  b.morph(),
				err: ServerInternalErr(e),
			}
		},

		"error/status-unknown": func(t *testing.T) test {
			ch, err := newHTTPCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = "unknown"
			e := errors.New("unknown challenge state: unknown")
			return test{
				ch:  b.morph(),
				err: ServerInternalErr(e),
			}
		},

		"ok/http-get-error": func(t *testing.T) test {
			ch, err := newHTTPCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing
			ch = b.morph()

			rch := ch.clone()
			geterr := errors.New("force")
			url := fmt.Sprintf("http://%s/.well-known/acme-challenge/%s", ch.getValue(), ch.getToken())
			e := errors.Wrapf(geterr, "error doing http GET for url %s", url)
			rch.Error = ConnectionErr(e).ToACME()

			return test{
				ch: ch,
				vo: validateOptions{
					httpGet: func(url string) (*http.Response, error) {
						return nil, geterr
					},
				},
				res: rch,
			}
		},

		"processing/http-get->=400": func(t *testing.T) test {
			ch, err := newHTTPCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing
			ch = b.morph()

			rch := ch.clone()
			url := fmt.Sprintf("http://%s/.well-known/acme-challenge/%s", ch.getValue(), ch.getToken())
			e := errors.Errorf("error doing http GET for url %s with status code %d", url, http.StatusBadRequest)
			rch.Error = ConnectionErr(e).ToACME()

			return test{
				ch: ch,
				vo: validateOptions{
					httpGet: func(url string) (*http.Response, error) {
						return &http.Response{
							Body:       ioutil.NopCloser(bytes.NewBufferString("")),
							StatusCode: http.StatusBadRequest,
						}, nil
					},
				},
				res: rch,
			}
		},

		"processing/read-body-error": func(t *testing.T) test {
			ch, err := newHTTPCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing
			ch = b.morph()

			rch := ch.clone()
			url := fmt.Sprintf("http://%s/.well-known/acme-challenge/%s", ch.getValue(), ch.getToken())
			e := errors.Wrapf(errors.New("force"), "error reading response body for url %s", url)
			rch.Error = ServerInternalErr(e).ToACME()

			return test{
				ch: ch,
				vo: validateOptions{
					httpGet: func(url string) (*http.Response, error) {
						return &http.Response{
							Body: errReader(0),
						}, nil
					},
				},
				res: rch,
			}
		},

		"error/key-authorization-gen": func(t *testing.T) test {
			ch, err := newHTTPCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing
			ch = b.morph()

			jwk, err := jose.GenerateJWK("EC", "P-256", "ES256", "sig", "", 0)
			assert.FatalError(t, err)
			jwk.Key = "foo"

			return test{
				ch: ch,
				vo: validateOptions{
					httpGet: func(url string) (*http.Response, error) {
						return &http.Response{
							Body: ioutil.NopCloser(bytes.NewBufferString("foo")),
						}, nil
					},
				},
				jwk: jwk,
				err: ServerInternalErr(errors.New("error generating JWK thumbprint: square/go-jose: unknown key type 'string'")),
			}
		},

		"invalid/key-auth-mismatch": func(t *testing.T) test {
			ch, err := newHTTPCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing
			ch = b.morph()

			jwk, err := jose.GenerateJWK("EC", "P-256", "ES256", "sig", "", 0)
			assert.FatalError(t, err)
			expKeyAuth, err := KeyAuthorization(ch.getToken(), jwk)
			assert.FatalError(t, err)

			b = ch.clone()
			e := errors.Errorf("keyAuthorization does not match; expected %s, but got foo", expKeyAuth)
			b.Error = IncorrectResponseErr(e).ToACME()
			b.Retry = nil
			b.Status = StatusInvalid
			rch := b.morph()

			return test{
				ch: ch,
				vo: validateOptions{
					httpGet: func(url string) (*http.Response, error) {
						return &http.Response{
							Body: ioutil.NopCloser(bytes.NewBufferString("foo")),
						}, nil
					},
				},
				jwk: jwk,
				res: rch,
			}
		},

		"valid/normal-http-get": func(t *testing.T) test {
			ch, err := newHTTPCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing
			ch = b.morph()

			jwk, err := jose.GenerateJWK("EC", "P-256", "ES256", "sig", "", 0)
			assert.FatalError(t, err)
			expKeyAuth, err := KeyAuthorization(ch.getToken(), jwk)
			assert.FatalError(t, err)

			b = ch.clone()
			b.Validated = clock.Now()
			b.Status = StatusValid
			b.Error = nil
			b.Retry = nil
			rch := b.morph()

			return test{
				ch: ch,
				vo: validateOptions{
					httpGet: func(url string) (*http.Response, error) {
						return &http.Response{
							Body: ioutil.NopCloser(bytes.NewBufferString(expKeyAuth)),
						}, nil
					},
				},
				jwk: jwk,
				res: rch,
			}
		},
	}

	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			tc := run(t)
			if ch, err := tc.ch.validate(tc.jwk, tc.vo); err != nil {
				if assert.NotNil(t, tc.err) {
					ae, ok := err.(*Error)
					assert.True(t, ok)
					assert.HasPrefix(t, ae.Error(), tc.err.Error())
					assert.Equals(t, ae.StatusCode(), tc.err.StatusCode())
					assert.Equals(t, ae.Type, tc.err.Type)
				}
			} else {
				if assert.Nil(t, tc.err) {
					assert.Equals(t, tc.res.getID(), ch.getID())
					assert.Equals(t, tc.res.getAccountID(), ch.getAccountID())
					assert.Equals(t, tc.res.getAuthzID(), ch.getAuthzID())
					assert.Equals(t, tc.res.getStatus(), ch.getStatus())
					assert.Equals(t, tc.res.getToken(), ch.getToken())
					assert.Equals(t, tc.res.getCreated(), ch.getCreated())
					if tc.res.getValidated() != ch.getValidated() {
						now := clock.Now()
						window := now.Sub(tc.res.getValidated())
						assert.True(t, now.Sub(ch.getValidated()) <= window,
							"validated timestamp should come before now but after test case setup")
					} else {
						assert.Equals(t, tc.res.getValidated(), ch.getValidated())
					}
					assert.Equals(t, tc.res.getError(), ch.getError())
					assert.Equals(t, tc.res.getRetry(), ch.getRetry())
				}
			}
		})
	}
}

func TestTLSALPN01Validate(t *testing.T) {
	type test struct {
		srv *httptest.Server
		vo  validateOptions
		ch  challenge
		res challenge
		jwk *jose.JSONWebKey
		err *Error
	}
	tests := map[string]func(t *testing.T) test{

		"valid/status-noop": func(t *testing.T) test {
			ch, err := newTLSALPNCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusValid
			ch = b.morph()
			return test{
				ch:  ch,
				res: ch,
			}
		},

		"invalid/status-noop": func(t *testing.T) test {
			ch, err := newTLSALPNCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusInvalid
			ch = b.morph()
			return test{
				ch:  ch,
				res: ch,
			}
		},

		"processing/tls-dial-error": func(t *testing.T) test {
			ch, err := newTLSALPNCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing

			a := b.clone()
			e := (errors.Errorf("error doing TLS dial for %v:443: force", ch.getValue()))
			a.Error = ConnectionErr(e).ToACME()

			return test{
				ch: b.morph(),
				vo: validateOptions{
					tlsDial: func(network, addr string, config *tls.Config) (*tls.Conn, error) {
						return nil, errors.New("force")
					},
				},
				res: a.morph(),
			}
		},

		"processing/timeout": func(t *testing.T) test {
			ch, err := newTLSALPNCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing

			a := b.clone()
			e := errors.Errorf("error doing TLS dial for %v:443: tls: DialWithDialer timed out", ch.getValue())
			a.Error = ConnectionErr(e).ToACME()

			srv, tlsDial := newTestTLSALPNServer(nil)
			// srv.Start() - do not start server to cause timeout

			return test{
				srv: srv,
				ch:  b.morph(),
				vo: validateOptions{
					tlsDial: tlsDial,
				},
				res: a.morph(),
			}
		},

		"processing/no-certificates": func(t *testing.T) test {
			ch, err := newTLSALPNCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing

			a := b.clone()
			e := errors.Errorf("tls-alpn-01 challenge for %v resulted in no certificates", ch.getValue())
			a.Error = TLSErr(e).ToACME()

			return test{
				ch: b.morph(),
				vo: validateOptions{
					tlsDial: func(network, addr string, config *tls.Config) (*tls.Conn, error) {
						return tls.Client(&noopConn{}, config), nil
					},
				},
				res: a.morph(),
			}
		},

		"processing/no-protocol": func(t *testing.T) test {
			ch, err := newTLSALPNCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing

			a := b.clone()
			e := errors.New("cannot negotiate ALPN acme-tls/1 protocol for tls-alpn-01 challenge")
			a.Error = TLSErr(e).ToACME()

			jwk, err := jose.GenerateJWK("EC", "P-256", "ES256", "sig", "", 0)
			assert.FatalError(t, err)

			srv := httptest.NewTLSServer(nil)

			return test{
				srv: srv,
				ch:  b.morph(),
				vo: validateOptions{
					tlsDial: func(network, addr string, config *tls.Config) (*tls.Conn, error) {
						return tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", srv.Listener.Addr().String(), config)
					},
				},
				jwk: jwk,
				res: a.morph(),
			}
		},

		"processing/no-names": func(t *testing.T) test {
			ch, err := newTLSALPNCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing

			a := b.clone()
			e := errors.Errorf("incorrect certificate for tls-alpn-01 challenge: leaf certificate must contain a single DNS name, %v", ch.getValue())
			a.Error = TLSErr(e).ToACME()

			jwk, err := jose.GenerateJWK("EC", "P-256", "ES256", "sig", "", 0)
			assert.FatalError(t, err)
			expKeyAuth, err := KeyAuthorization(ch.getToken(), jwk)
			assert.FatalError(t, err)
			expKeyAuthHash := sha256.Sum256([]byte(expKeyAuth))

			cert, err := newTLSALPNValidationCert(expKeyAuthHash[:], false, true)
			assert.FatalError(t, err)
			srv, tlsDial := newTestTLSALPNServer(cert)
			srv.Start()

			return test{
				srv: srv,
				ch:  b.morph(),
				vo: validateOptions{
					tlsDial: tlsDial,
				},
				jwk: jwk,
				res: a.morph(),
			}
		},

		"processing/too-many-names": func(t *testing.T) test {
			ch, err := newTLSALPNCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing

			a := b.clone()
			e := errors.Errorf("incorrect certificate for tls-alpn-01 challenge: leaf certificate must contain a single DNS name, %v", ch.getValue())
			a.Error = TLSErr(e).ToACME()

			jwk, err := jose.GenerateJWK("EC", "P-256", "ES256", "sig", "", 0)
			assert.FatalError(t, err)
			expKeyAuth, err := KeyAuthorization(ch.getToken(), jwk)
			assert.FatalError(t, err)
			expKeyAuthHash := sha256.Sum256([]byte(expKeyAuth))

			cert, err := newTLSALPNValidationCert(expKeyAuthHash[:], false, true, ch.getValue(), "other.internal")
			assert.FatalError(t, err)
			srv, tlsDial := newTestTLSALPNServer(cert)
			srv.Start()

			return test{
				srv: srv,
				ch:  b.morph(),
				vo: validateOptions{
					tlsDial: tlsDial,
				},
				jwk: jwk,
				res: a.morph(),
			}
		},

		"processing/wrong-name": func(t *testing.T) test {
			ch, err := newTLSALPNCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing

			a := b.clone()
			e := errors.Errorf("incorrect certificate for tls-alpn-01 challenge: leaf certificate must contain a single DNS name, %v", ch.getValue())
			a.Error = TLSErr(e).ToACME()

			jwk, err := jose.GenerateJWK("EC", "P-256", "ES256", "sig", "", 0)
			assert.FatalError(t, err)
			expKeyAuth, err := KeyAuthorization(ch.getToken(), jwk)
			assert.FatalError(t, err)
			expKeyAuthHash := sha256.Sum256([]byte(expKeyAuth))

			cert, err := newTLSALPNValidationCert(expKeyAuthHash[:], false, true, "other.internal")
			assert.FatalError(t, err)
			srv, tlsDial := newTestTLSALPNServer(cert)
			srv.Start()

			return test{
				srv: srv,
				ch:  b.morph(),
				vo: validateOptions{
					tlsDial: tlsDial,
				},
				jwk: jwk,
				res: a.morph(),
			}
		},

		"processing/no-extension": func(t *testing.T) test {
			ch, err := newTLSALPNCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing

			a := b.clone()
			e := errors.New("incorrect certificate for tls-alpn-01 challenge: missing acmeValidationV1 extension")
			a.Error = IncorrectResponseErr(e).ToACME()
			jwk, err := jose.GenerateJWK("EC", "P-256", "ES256", "sig", "", 0)
			assert.FatalError(t, err)

			cert, err := newTLSALPNValidationCert(nil, false, true, ch.getValue())
			assert.FatalError(t, err)
			srv, tlsDial := newTestTLSALPNServer(cert)
			srv.Start()

			return test{
				srv: srv,
				ch:  b.morph(),
				vo: validateOptions{
					tlsDial: tlsDial,
				},
				jwk: jwk,
				res: a.morph(),
			}
		},

		"processing/extension-not-critical": func(t *testing.T) test {
			ch, err := newTLSALPNCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing

			a := b.clone()
			e := errors.New("incorrect certificate for tls-alpn-01 challenge: acmeValidationV1 extension not critical")
			a.Error = IncorrectResponseErr(e).ToACME()

			jwk, err := jose.GenerateJWK("EC", "P-256", "ES256", "sig", "", 0)
			assert.FatalError(t, err)
			expKeyAuth, err := KeyAuthorization(ch.getToken(), jwk)
			assert.FatalError(t, err)
			expKeyAuthHash := sha256.Sum256([]byte(expKeyAuth))

			cert, err := newTLSALPNValidationCert(expKeyAuthHash[:], false, false, ch.getValue())
			assert.FatalError(t, err)
			srv, tlsDial := newTestTLSALPNServer(cert)
			srv.Start()

			return test{
				srv: srv,
				ch:  b.morph(),
				vo: validateOptions{
					tlsDial: tlsDial,
				},
				jwk: jwk,
				res: a.morph(),
			}
		},

		"processing/extension-malformed": func(t *testing.T) test {
			ch, err := newTLSALPNCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing

			a := b.clone()
			e := errors.New("incorrect certificate for tls-alpn-01 challenge: malformed acmeValidationV1 extension value")
			a.Error = IncorrectResponseErr(e).ToACME()

			jwk, err := jose.GenerateJWK("EC", "P-256", "ES256", "sig", "", 0)
			assert.FatalError(t, err)

			cert, err := newTLSALPNValidationCert([]byte{1, 2, 3}, false, true, ch.getValue())
			assert.FatalError(t, err)
			srv, tlsDial := newTestTLSALPNServer(cert)
			srv.Start()

			return test{
				srv: srv,
				ch:  b.morph(),
				vo: validateOptions{
					tlsDial: tlsDial,
				},
				jwk: jwk,
				res: a.morph(),
			}
		},

		"invalid/mismatched-token": func(t *testing.T) test {
			ch, err := newTLSALPNCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing

			jwk, err := jose.GenerateJWK("EC", "P-256", "ES256", "sig", "", 0)
			assert.FatalError(t, err)
			expKeyAuth, err := KeyAuthorization(ch.getToken(), jwk)
			assert.FatalError(t, err)
			expKeyAuthHash := sha256.Sum256([]byte(expKeyAuth))
			incorrectTokenHash := sha256.Sum256([]byte("mismatched"))

			a := b.clone()
			e := errors.Errorf("incorrect certificate for tls-alpn-01 challenge: "+
				"expected acmeValidationV1 extension value %s for this challenge but got %s",
				hex.EncodeToString(expKeyAuthHash[:]), hex.EncodeToString(incorrectTokenHash[:]))
			a.Error = IncorrectResponseErr(e).ToACME()
			a.Status = StatusInvalid

			cert, err := newTLSALPNValidationCert(incorrectTokenHash[:], false, true, ch.getValue())
			assert.FatalError(t, err)
			srv, tlsDial := newTestTLSALPNServer(cert)
			srv.Start()

			return test{
				srv: srv,
				ch:  b.morph(),
				vo: validateOptions{
					tlsDial: tlsDial,
				},
				jwk: jwk,
				res: a.morph(),
			}
		},

		"processing/obsolete-oid": func(t *testing.T) test {
			ch, err := newTLSALPNCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing

			jwk, err := jose.GenerateJWK("EC", "P-256", "ES256", "sig", "", 0)
			assert.FatalError(t, err)

			a := b.clone()
			e := errors.New("incorrect certificate for tls-alpn-01 challenge: " +
				"obsolete id-pe-acmeIdentifier in acmeValidationV1 extension")
			a.Error = IncorrectResponseErr(e).ToACME()

			expKeyAuth, err := KeyAuthorization(ch.getToken(), jwk)
			assert.FatalError(t, err)
			expKeyAuthHash := sha256.Sum256([]byte(expKeyAuth))

			cert, err := newTLSALPNValidationCert(expKeyAuthHash[:], true, true, ch.getValue())
			assert.FatalError(t, err)
			srv, tlsDial := newTestTLSALPNServer(cert)
			srv.Start()

			return test{
				srv: srv,
				ch:  b.morph(),
				vo: validateOptions{
					tlsDial: tlsDial,
				},
				jwk: jwk,
				res: a.morph(),
			}
		},

		"valid/expected-identifier": func(t *testing.T) test {
			ch, err := newTLSALPNCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing

			jwk, err := jose.GenerateJWK("EC", "P-256", "ES256", "sig", "", 0)
			assert.FatalError(t, err)
			expKeyAuth, err := KeyAuthorization(ch.getToken(), jwk)
			assert.FatalError(t, err)
			expKeyAuthHash := sha256.Sum256([]byte(expKeyAuth))

			a := b.clone()
			a.Validated = clock.Now()
			a.Status = StatusValid
			a.Error = nil
			a.Retry = nil

			cert, err := newTLSALPNValidationCert(expKeyAuthHash[:], false, true, ch.getValue())
			assert.FatalError(t, err)
			srv, tlsDial := newTestTLSALPNServer(cert)
			srv.Start()

			return test{
				srv: srv,
				ch:  b.morph(),
				vo: validateOptions{
					tlsDial: func(network, addr string, config *tls.Config) (conn *tls.Conn, err error) {
						assert.Equals(t, network, "tcp")
						assert.Equals(t, addr, net.JoinHostPort(ch.getValue(), "443"))
						assert.Equals(t, config.NextProtos, []string{"acme-tls/1"})
						assert.Equals(t, config.ServerName, ch.getValue())
						assert.True(t, config.InsecureSkipVerify)

						return tlsDial(network, addr, config)
					},
				},
				jwk: jwk,
				res: a.morph(),
			}
		},
	}

	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			tc := run(t)

			if tc.srv != nil {
				defer tc.srv.Close()
			}

			if ch, err := tc.ch.validate(tc.jwk, tc.vo); err != nil {
				if assert.NotNil(t, tc.err) {
					ae, ok := err.(*Error)
					assert.True(t, ok)
					assert.HasPrefix(t, ae.Error(), tc.err.Error())
					assert.Equals(t, ae.StatusCode(), tc.err.StatusCode())
					assert.Equals(t, ae.Type, tc.err.Type)
				}
			} else {
				if assert.Nil(t, tc.err) {
					assert.Equals(t, tc.res.getID(), ch.getID())
					assert.Equals(t, tc.res.getAccountID(), ch.getAccountID())
					assert.Equals(t, tc.res.getAuthzID(), ch.getAuthzID())
					assert.Equals(t, tc.res.getStatus(), ch.getStatus())
					assert.Equals(t, tc.res.getToken(), ch.getToken())
					assert.Equals(t, tc.res.getCreated(), ch.getCreated())
					assert.Equals(t, tc.res.getValidated(), ch.getValidated())
					if tc.res.getValidated() != ch.getValidated() {
						now := clock.Now()
						window := now.Sub(tc.res.getValidated())
						assert.True(t, now.Sub(ch.getValidated()) <= window,
							"validated timestamp should come before now but after test case setup")
					} else {
						assert.Equals(t, tc.res.getValidated(), ch.getValidated())
					}
					assert.Equals(t, tc.res.getError(), ch.getError())
					assert.Equals(t, tc.res.getRetry(), ch.getRetry())
				}
			}
		})
	}
}

func newTestTLSALPNServer(validationCert *tls.Certificate) (*httptest.Server, tlsDialer) {
	srv := httptest.NewUnstartedServer(http.NewServeMux())

	srv.Config.TLSNextProto = map[string]func(*http.Server, *tls.Conn, http.Handler){
		"acme-tls/1": func(_ *http.Server, conn *tls.Conn, _ http.Handler) {
			// no-op
		},
		"http/1.1": func(_ *http.Server, conn *tls.Conn, _ http.Handler) {
			panic("unexpected http/1.1 next proto")
		},
	}

	srv.TLS = &tls.Config{
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			if len(hello.SupportedProtos) == 1 && hello.SupportedProtos[0] == "acme-tls/1" {
				return validationCert, nil
			}
			return nil, nil
		},
		NextProtos: []string{
			"acme-tls/1",
			"http/1.1",
		},
	}

	srv.Listener = tls.NewListener(srv.Listener, srv.TLS)
	//srv.Config.ErrorLog = log.New(ioutil.Discard, "", 0) // hush

	return srv, func(network, addr string, config *tls.Config) (conn *tls.Conn, err error) {
		return tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", srv.Listener.Addr().String(), config)
	}
}

// noopConn is a mock net.Conn that does nothing.
type noopConn struct{}

func (c *noopConn) Read(_ []byte) (n int, err error)   { return 0, io.EOF }
func (c *noopConn) Write(_ []byte) (n int, err error)  { return 0, io.EOF }
func (c *noopConn) Close() error                       { return nil }
func (c *noopConn) LocalAddr() net.Addr                { return &net.IPAddr{IP: net.IPv4zero, Zone: ""} }
func (c *noopConn) RemoteAddr() net.Addr               { return &net.IPAddr{IP: net.IPv4zero, Zone: ""} }
func (c *noopConn) SetDeadline(t time.Time) error      { return nil }
func (c *noopConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *noopConn) SetWriteDeadline(t time.Time) error { return nil }

func newTLSALPNValidationCert(keyAuthHash []byte, obsoleteOID, critical bool, names ...string) (*tls.Certificate, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	certTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1337),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(0, 0, 1),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              names,
	}

	if keyAuthHash != nil {
		oid := asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 1, 31}
		if obsoleteOID {
			oid = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 1, 30, 1}
		}

		keyAuthHashEnc, _ := asn1.Marshal(keyAuthHash[:])

		certTemplate.ExtraExtensions = []pkix.Extension{
			{
				Id:       oid,
				Critical: critical,
				Value:    keyAuthHashEnc,
			},
		}
	}

	cert, err := x509.CreateCertificate(rand.Reader, certTemplate, certTemplate, privateKey.Public(), privateKey)
	if err != nil {
		return nil, err
	}

	return &tls.Certificate{
		PrivateKey:  privateKey,
		Certificate: [][]byte{cert},
	}, nil
}

func TestDNS01Validate(t *testing.T) {
	type test struct {
		vo  validateOptions
		ch  challenge
		res challenge
		jwk *jose.JSONWebKey
		err *Error
	}
	tests := map[string]func(t *testing.T) test{

		"valid/status-noop": func(t *testing.T) test {
			ch, err := newDNSCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusValid
			ch = b.morph()
			return test{
				ch:  ch,
				res: ch,
			}
		},

		"invalid/status-noop": func(t *testing.T) test {
			ch, err := newDNSCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusInvalid
			ch = b.morph()
			return test{
				ch:  ch,
				res: ch,
			}
		},

		"error/status-pending": func(t *testing.T) test {
			ch, err := newDNSCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusPending
			e := errors.New("pending challenges must first be moved to the processing state")
			return test{
				ch:  b.morph(),
				err: ServerInternalErr(e),
			}
		},

		"error/status-unknown": func(t *testing.T) test {
			ch, err := newDNSCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = "unknown"
			e := errors.New("unknown challenge state: unknown")
			return test{
				ch:  b.morph(),
				err: ServerInternalErr(e),
			}
		},

		"processing/lookup-txt-error": func(t *testing.T) test {
			ch, err := newDNSCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing
			ch = b.morph()

			b = ch.clone()
			e := errors.Errorf("error looking up TXT records for domain %s: force", ch.getValue())
			b.Error = DNSErr(e).ToACME()
			rch := b.morph()

			return test{
				ch: ch,
				vo: validateOptions{
					lookupTxt: func(url string) ([]string, error) {
						return nil, errors.New("force")
					},
				},
				res: rch,
			}
		},

		"fail/key-authorization-gen-error": func(t *testing.T) test {
			ch, err := newDNSCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing
			ch = b.morph()

			jwk, err := jose.GenerateJWK("EC", "P-256", "ES256", "sig", "", 0)
			assert.FatalError(t, err)
			jwk.Key = "foo"

			return test{
				ch: ch,
				vo: validateOptions{
					lookupTxt: func(url string) ([]string, error) {
						return []string{"foo", "bar"}, nil
					},
				},
				jwk: jwk,
				err: ServerInternalErr(errors.New("error generating JWK thumbprint: square/go-jose: unknown key type 'string'")),
			}
		},

		"invalid/key-auth-mismatch": func(t *testing.T) test {
			ch, err := newDNSCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing
			ch = b.morph()

			jwk, err := jose.GenerateJWK("EC", "P-256", "ES256", "sig", "", 0)
			assert.FatalError(t, err)
			expKeyAuth, err := KeyAuthorization(ch.getToken(), jwk)
			assert.FatalError(t, err)

			e := errors.Errorf("keyAuthorization does not match; "+
				"expected %s, but got %s", expKeyAuth, []string{"foo", "bar"})
			b = ch.clone()
			b.Status = StatusInvalid
			b.Error = IncorrectResponseErr(e).ToACME()
			rch := b.morph()

			return test{
				ch: ch,
				vo: validateOptions{
					lookupTxt: func(url string) ([]string, error) {
						return []string{"foo", "bar"}, nil
					},
				},
				jwk: jwk,
				res: rch,
			}
		},

		"processing/empty-list": func(t *testing.T) test {
			ch, err := newDNSCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing
			ch = b.morph()

			jwk, err := jose.GenerateJWK("EC", "P-256", "ES256", "sig", "", 0)
			assert.FatalError(t, err)

			e := errors.New("no TXT record found at '_acme-challenge.zap.internal'")
			b = ch.clone()
			b.Error = DNSErr(e).ToACME()
			rch := b.morph()

			return test{
				ch: ch,
				vo: validateOptions{
					lookupTxt: func(url string) ([]string, error) {
						return []string{}, nil
					},
				},
				jwk: jwk,
				res: rch,
			}
		},

		"valid/lookup-txt-normal": func(t *testing.T) test {
			ch, err := newDNSCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing
			ch = b.morph()

			jwk, err := jose.GenerateJWK("EC", "P-256", "ES256", "sig", "", 0)
			assert.FatalError(t, err)
			expKeyAuth, err := KeyAuthorization(ch.getToken(), jwk)
			assert.FatalError(t, err)
			h := sha256.Sum256([]byte(expKeyAuth))
			expected := base64.RawURLEncoding.EncodeToString(h[:])

			b = ch.clone()
			b.Validated = clock.Now()
			b.Status = StatusValid
			b.Error = nil
			b.Retry = nil
			rch := b.morph()

			return test{
				ch: ch,
				vo: validateOptions{
					lookupTxt: func(url string) ([]string, error) {
						return []string{"foo", expected}, nil
					},
				},
				jwk: jwk,
				res: rch,
			}
		},

		"valid/lookup-txt-wildcard": func(t *testing.T) test {
			ch, err := newDNSCh()
			assert.FatalError(t, err)
			b := ch.clone()
			b.Status = StatusProcessing
			b.Value = "*.zap.internal"
			ch = b.morph()

			jwk, err := jose.GenerateJWK("EC", "P-256", "ES256", "sig", "", 0)
			assert.FatalError(t, err)

			expKeyAuth, err := KeyAuthorization(ch.getToken(), jwk)
			assert.FatalError(t, err)
			h := sha256.Sum256([]byte(expKeyAuth))
			expected := base64.RawURLEncoding.EncodeToString(h[:])

			b = ch.clone()
			b.Status = StatusValid
			b.Validated = clock.Now()
			b.Error = nil
			b.Retry = nil
			rch := b.morph()

			return test{
				ch: ch,
				vo: validateOptions{
					lookupTxt: func(url string) ([]string, error) {
						assert.Equals(t, url, "_acme-challenge.zap.internal")
						return []string{"foo", expected}, nil
					},
				},
				jwk: jwk,
				res: rch,
			}
		},
	}

	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			tc := run(t)
			if ch, err := tc.ch.validate(tc.jwk, tc.vo); err != nil {
				if assert.NotNil(t, tc.err) {
					ae, ok := err.(*Error)
					assert.True(t, ok)
					assert.HasPrefix(t, ae.Error(), tc.err.Error())
					assert.Equals(t, ae.StatusCode(), tc.err.StatusCode())
					assert.Equals(t, ae.Type, tc.err.Type)
				}
			} else {
				if assert.Nil(t, tc.err) {
					assert.Equals(t, tc.res.getID(), ch.getID())
					assert.Equals(t, tc.res.getAccountID(), ch.getAccountID())
					assert.Equals(t, tc.res.getAuthzID(), ch.getAuthzID())
					assert.Equals(t, tc.res.getStatus(), ch.getStatus())
					assert.Equals(t, tc.res.getToken(), ch.getToken())
					assert.Equals(t, tc.res.getCreated(), ch.getCreated())
					if tc.res.getValidated() != ch.getValidated() {
						now := clock.Now()
						window := now.Sub(tc.res.getValidated())
						assert.True(t, now.Sub(ch.getValidated()) <= window,
							"validated timestamp should come before now but after test case setup")
					} else {
						assert.Equals(t, tc.res.getValidated(), ch.getValidated())
					}
					assert.Equals(t, tc.res.getError(), ch.getError())
					assert.Equals(t, tc.res.getRetry(), ch.getRetry())
				}
			}
		})
	}
}
