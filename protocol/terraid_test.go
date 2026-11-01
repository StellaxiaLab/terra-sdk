package protocol

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	testTerraIDIssuer  = "https://id.terra.example"
	testTerraIDNodeID  = "node_1787923476493_4c773555dd1dd13d"
	testTerraIDAccount = "acc_01JBC8Z5Q0M6T9X2V4R7K3N5PW"
)

var testTerraIDNow = time.Date(2026, 10, 29, 0, 1, 0, 0, time.UTC)

func testTerraIDAudience(t *testing.T) string {
	t.Helper()
	aud, err := TreeAudience(testTerraIDNodeID)
	if err != nil {
		t.Fatalf("TreeAudience: %v", err)
	}
	return aud
}

func testTerraIDExpectation(t *testing.T, use string) TerraIDExpectation {
	return TerraIDExpectation{
		Issuer:                testTerraIDIssuer,
		Audience:              testTerraIDAudience(t),
		Use:                   use,
		Now:                   testTerraIDNow,
		RequireTerraIDSubject: true,
	}
}

// testTerraIDClaims is a valid claim set for use, issued a minute before now.
func testTerraIDClaims(t *testing.T, use string) TerraIDClaims {
	iat := testTerraIDNow.Add(-30 * time.Second).Unix()
	claims := TerraIDClaims{
		Issuer:   testTerraIDIssuer,
		Subject:  testTerraIDAccount,
		Audience: TerraIDAudience{testTerraIDAudience(t)},
		IssuedAt: iat,
		Use:      use,
		ACR:      TerraIDACRMultiFactor,
		AMR:      []string{"pwd", "otp"},
		AuthTime: iat - 10,
		Handle:   "marui",
	}
	switch use {
	case TerraIDUseTreeLogin:
		claims.ExpiresAt = iat + 300
	case TerraIDUseNodeEnrollCodeIssue:
		claims.ExpiresAt = iat + 60
		claims.JWTID = "assert-0001"
		claims.Actor = &TerraIDActor{Subject: "svc:terra-access"}
	case TerraIDUseExternalIdentityRevoke:
		claims.ExpiresAt = iat + 600
		claims.JWTID = "revoke-0001"
		claims.Reason = TerraIDRevokeReasonAccountCompromised
	}
	return claims
}

func TestTerraIDClaimsValid(t *testing.T) {
	for _, use := range []string{TerraIDUseTreeLogin, TerraIDUseNodeEnrollCodeIssue, TerraIDUseExternalIdentityRevoke} {
		if err := ValidateTerraIDClaims(testTerraIDClaims(t, use), testTerraIDExpectation(t, use)); err != nil {
			t.Fatalf("%s: valid claims refused: %v", use, err)
		}
	}
	// A login token without terra_use is a plain OIDC id_token and is accepted.
	login := testTerraIDClaims(t, TerraIDUseTreeLogin)
	login.Use = ""
	if err := ValidateTerraIDClaims(login, testTerraIDExpectation(t, TerraIDUseTreeLogin)); err != nil {
		t.Fatalf("login without terra_use refused: %v", err)
	}
	// A foreign IdP's opaque sub is accepted when the profile does not require acc_.
	foreign := testTerraIDClaims(t, TerraIDUseTreeLogin)
	foreign.Subject = "00u1abcd2EFGH3ijk4x7"
	expect := testTerraIDExpectation(t, TerraIDUseTreeLogin)
	expect.RequireTerraIDSubject = false
	if err := ValidateTerraIDClaims(foreign, expect); err != nil {
		t.Fatalf("opaque sub refused: %v", err)
	}
}

// TestTerraIDClaimsRejected is the §2.3 table: at least one refusal per row.
func TestTerraIDClaimsRejected(t *testing.T) {
	now := testTerraIDNow.Unix()
	cases := []struct {
		name   string
		use    string
		mutate func(*TerraIDClaims, *TerraIDExpectation)
	}{
		// iss
		{"iss differs", TerraIDUseTreeLogin, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.Issuer = "https://id.terra.example.evil" }},
		{"iss trailing slash", TerraIDUseTreeLogin, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.Issuer = testTerraIDIssuer + "/" }},
		{"iss case differs", TerraIDUseTreeLogin, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.Issuer = "https://ID.terra.example" }},
		{"iss empty", TerraIDUseTreeLogin, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.Issuer = "" }},
		// sub
		{"sub empty", TerraIDUseTreeLogin, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.Subject = "" }},
		{"sub not acc_ for Terra ID", TerraIDUseTreeLogin, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.Subject = "user-1" }},
		{"sub lowercase ulid", TerraIDUseTreeLogin, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.Subject = strings.ToLower(testTerraIDAccount) }},
		{"opaque sub empty", TerraIDUseTreeLogin, func(c *TerraIDClaims, e *TerraIDExpectation) {
			e.RequireTerraIDSubject = false
			c.Subject = ""
		}},
		{"opaque sub too long", TerraIDUseTreeLogin, func(c *TerraIDClaims, e *TerraIDExpectation) {
			e.RequireTerraIDSubject = false
			c.Subject = strings.Repeat("x", 256)
		}},
		// aud
		{"aud missing", TerraIDUseTreeLogin, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.Audience = nil }},
		{"aud other tree", TerraIDUseTreeLogin, func(c *TerraIDClaims, _ *TerraIDExpectation) {
			c.Audience = TerraIDAudience{"tree:node_1787923476493_0000000000000000"}
		}},
		{"aud two values", TerraIDUseTreeLogin, func(c *TerraIDClaims, _ *TerraIDExpectation) {
			c.Audience = append(c.Audience, "tree:node_1787923476493_0000000000000000")
		}},
		{"aud same value twice", TerraIDUseTreeLogin, func(c *TerraIDClaims, _ *TerraIDExpectation) {
			c.Audience = append(c.Audience, c.Audience[0])
		}},
		// exp
		{"exp missing", TerraIDUseTreeLogin, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.ExpiresAt = 0 }},
		{"exp past leeway", TerraIDUseTreeLogin, func(c *TerraIDClaims, _ *TerraIDExpectation) {
			c.ExpiresAt = now - 60
			c.IssuedAt = now - 300
		}},
		// iat
		{"iat missing", TerraIDUseTreeLogin, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.IssuedAt = 0 }},
		{"iat in future past leeway", TerraIDUseTreeLogin, func(c *TerraIDClaims, _ *TerraIDExpectation) {
			c.IssuedAt = now + 61
			c.ExpiresAt = now + 300
		}},
		{"exp equals iat", TerraIDUseTreeLogin, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.ExpiresAt = c.IssuedAt }},
		{"exp before iat", TerraIDUseTreeLogin, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.ExpiresAt = c.IssuedAt - 1 }},
		{"login lifetime over 600s", TerraIDUseTreeLogin, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.ExpiresAt = c.IssuedAt + 601 }},
		{"assertion lifetime over 60s", TerraIDUseNodeEnrollCodeIssue, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.ExpiresAt = c.IssuedAt + 61 }},
		{"revoke lifetime over 600s", TerraIDUseExternalIdentityRevoke, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.ExpiresAt = c.IssuedAt + 601 }},
		// nbf
		{"nbf in future past leeway", TerraIDUseTreeLogin, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.NotBefore = now + 61 }},
		// jti
		{"assertion jti missing", TerraIDUseNodeEnrollCodeIssue, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.JWTID = "" }},
		{"revoke jti missing", TerraIDUseExternalIdentityRevoke, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.JWTID = "" }},
		{"jti too long", TerraIDUseTreeLogin, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.JWTID = strings.Repeat("j", 129) }},
		// terra_use
		{"login with assertion use", TerraIDUseTreeLogin, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.Use = TerraIDUseNodeEnrollCodeIssue }},
		{"login with unknown use", TerraIDUseTreeLogin, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.Use = "tree.admin" }},
		{"assertion without use", TerraIDUseNodeEnrollCodeIssue, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.Use = "" }},
		{"assertion with login use", TerraIDUseNodeEnrollCodeIssue, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.Use = TerraIDUseTreeLogin }},
		{"assertion with other purpose", TerraIDUseNodeEnrollCodeIssue, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.Use = TerraIDUseExternalIdentityRevoke }},
		// auth_time
		{"auth_time in future", TerraIDUseTreeLogin, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.AuthTime = now + 61 }},
		// act
		{"act without sub", TerraIDUseNodeEnrollCodeIssue, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.Actor = &TerraIDActor{} }},
		// terra_reason
		{"revoke without reason", TerraIDUseExternalIdentityRevoke, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.Reason = "" }},
		{"revoke unknown reason", TerraIDUseExternalIdentityRevoke, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.Reason = "because" }},
		{"reason on login", TerraIDUseTreeLogin, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.Reason = TerraIDRevokeReasonOperator }},
		{"reason on enroll assertion", TerraIDUseNodeEnrollCodeIssue, func(c *TerraIDClaims, _ *TerraIDExpectation) { c.Reason = TerraIDRevokeReasonOperator }},
		// expectation that would accept "any"
		{"expect issuer empty", TerraIDUseTreeLogin, func(c *TerraIDClaims, e *TerraIDExpectation) { e.Issuer, c.Issuer = "", "" }},
		{"expect audience empty", TerraIDUseTreeLogin, func(c *TerraIDClaims, e *TerraIDExpectation) {
			e.Audience = ""
			c.Audience = TerraIDAudience{""}
		}},
		{"expect use empty", TerraIDUseTreeLogin, func(_ *TerraIDClaims, e *TerraIDExpectation) { e.Use = "" }},
		{"expect use unknown", TerraIDUseTreeLogin, func(_ *TerraIDClaims, e *TerraIDExpectation) { e.Use = "tree.admin" }},
		{"expect now zero", TerraIDUseTreeLogin, func(_ *TerraIDClaims, e *TerraIDExpectation) { e.Now = time.Time{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claims := testTerraIDClaims(t, tc.use)
			expect := testTerraIDExpectation(t, tc.use)
			tc.mutate(&claims, &expect)
			err := ValidateTerraIDClaims(claims, expect)
			if !errors.Is(err, ErrTerraIDTokenInvalid) {
				t.Fatalf("want ErrTerraIDTokenInvalid, got %v", err)
			}
		})
	}
}

func TestTerraIDClaimsLeewayBoundaries(t *testing.T) {
	now := testTerraIDNow.Unix()
	check := func(t *testing.T, name string, mutate func(*TerraIDClaims), wantOK bool) {
		t.Helper()
		claims := testTerraIDClaims(t, TerraIDUseTreeLogin)
		mutate(&claims)
		err := ValidateTerraIDClaims(claims, testTerraIDExpectation(t, TerraIDUseTreeLogin))
		if wantOK && err != nil {
			t.Errorf("%s: want accepted, got %v", name, err)
		}
		if !wantOK && !errors.Is(err, ErrTerraIDTokenInvalid) {
			t.Errorf("%s: want refused, got %v", name, err)
		}
	}
	// exp: now - leeway < exp.
	check(t, "exp = now-59", func(c *TerraIDClaims) { c.IssuedAt, c.ExpiresAt = now-300, now-59 }, true)
	check(t, "exp = now-60", func(c *TerraIDClaims) { c.IssuedAt, c.ExpiresAt = now-300, now-60 }, false)
	// iat: iat <= now + leeway.
	check(t, "iat = now+60", func(c *TerraIDClaims) { c.IssuedAt, c.ExpiresAt = now+60, now+300 }, true)
	check(t, "iat = now+61", func(c *TerraIDClaims) { c.IssuedAt, c.ExpiresAt = now+61, now+300 }, false)
	// nbf: nbf <= now + leeway.
	check(t, "nbf = now+60", func(c *TerraIDClaims) { c.NotBefore = now + 60 }, true)
	check(t, "nbf = now+61", func(c *TerraIDClaims) { c.NotBefore = now + 61 }, false)
	// lifetime cap is inclusive.
	check(t, "lifetime = 600", func(c *TerraIDClaims) { c.ExpiresAt = c.IssuedAt + 600 }, true)
	check(t, "lifetime = 601", func(c *TerraIDClaims) { c.ExpiresAt = c.IssuedAt + 601 }, false)

	assertion := testTerraIDClaims(t, TerraIDUseNodeEnrollCodeIssue)
	assertion.ExpiresAt = assertion.IssuedAt + 60
	if err := ValidateTerraIDClaims(assertion, testTerraIDExpectation(t, TerraIDUseNodeEnrollCodeIssue)); err != nil {
		t.Errorf("assertion lifetime = 60: %v", err)
	}
}

func TestTerraIDMultiFactor(t *testing.T) {
	cases := map[string]bool{
		TerraIDACRMultiFactor:          true,
		TerraIDACRSingleFactor:         false,
		"":                             false,
		"URN:TERRA:ACR:MFA":            false,
		"terra:acr:mfa":                false,
		"urn:mace:incommon:iap:silver": false,
	}
	for acr, want := range cases {
		claims := TerraIDClaims{ACR: acr, AMR: []string{"pwd", "otp", "hwk"}}
		if got := claims.MultiFactor(); got != want {
			t.Errorf("acr %q: MultiFactor = %v, want %v (amr must not count)", acr, got, want)
		}
	}
}

func TestTerraIDAudienceJSON(t *testing.T) {
	aud := testTerraIDAudience(t)
	for _, raw := range []string{`"` + aud + `"`, `["` + aud + `"]`} {
		var got TerraIDAudience
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if len(got) != 1 || got[0] != aud {
			t.Fatalf("%s: got %v", raw, got)
		}
	}
	var two TerraIDAudience
	if err := json.Unmarshal([]byte(`["a","b"]`), &two); err != nil || len(two) != 2 {
		t.Fatalf("array of two: %v %v", two, err)
	}
	for _, raw := range []string{`7`, `{"a":1}`, `[1]`} {
		var bad TerraIDAudience
		if err := json.Unmarshal([]byte(raw), &bad); err == nil {
			t.Errorf("%s: want error", raw)
		}
	}

	single, _ := json.Marshal(TerraIDAudience{aud})
	if string(single) != `"`+aud+`"` {
		t.Errorf("single audience marshals as %s", single)
	}
	many, _ := json.Marshal(TerraIDAudience{"a", "b"})
	if string(many) != `["a","b"]` {
		t.Errorf("two audiences marshal as %s", many)
	}

	// Claims decoded from an array-valued aud with two entries are refused.
	payload := `{"iss":"` + testTerraIDIssuer + `","sub":"` + testTerraIDAccount + `","aud":["` + aud + `","tree:node_x1"],"exp":1,"iat":1}`
	var claims TerraIDClaims
	if err := json.Unmarshal([]byte(payload), &claims); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTerraIDClaims(claims, testTerraIDExpectation(t, TerraIDUseTreeLogin)); !errors.Is(err, ErrTerraIDTokenInvalid) {
		t.Fatalf("two-valued aud: %v", err)
	}
}

func TestTerraIDClaimsJSONNames(t *testing.T) {
	claims := testTerraIDClaims(t, TerraIDUseExternalIdentityRevoke)
	claims.NotBefore = claims.IssuedAt
	claims.Actor = &TerraIDActor{Subject: "svc:terra-access"}
	raw, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	var members map[string]any
	if err := json.Unmarshal(raw, &members); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"iss", "sub", "aud", "exp", "iat", "nbf", "jti", "terra_use", "acr", "amr",
		"auth_time", "terra_handle", "act", "terra_reason"} {
		if _, ok := members[name]; !ok {
			t.Errorf("claim %q missing from %s", name, raw)
		}
	}
	if _, ok := members["email"]; ok {
		t.Errorf("email must not be a contract claim")
	}
	var back TerraIDClaims
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Actor == nil || back.Actor.Subject != "svc:terra-access" || back.Reason != claims.Reason || back.Audience[0] != claims.Audience[0] {
		t.Fatalf("round trip lost data: %+v", back)
	}
}

func TestValidTerraAccountID(t *testing.T) {
	valid := []string{testTerraIDAccount, "acc_" + strings.Repeat("0", 26), "acc_7ZZZZZZZZZZZZZZZZZZZZZZZZZ"}
	for _, id := range valid {
		if !ValidTerraAccountID(id) {
			t.Errorf("%q: want valid", id)
		}
	}
	invalid := []string{
		"",
		"acc_",
		"acc_01JBC8Z5Q0M6T9X2V4R7K3N5P",   // 25
		"acc_01JBC8Z5Q0M6T9X2V4R7K3N5PWX", // 27
		"acc_01JBC8Z5Q0M6T9X2V4R7K3N5PI",  // I
		"acc_01JBC8Z5Q0M6T9X2V4R7K3N5PL",  // L
		"acc_01JBC8Z5Q0M6T9X2V4R7K3N5PO",  // O
		"acc_01JBC8Z5Q0M6T9X2V4R7K3N5PU",  // U
		"acc_01jbc8z5q0m6t9x2v4r7k3n5pw",  // lowercase
		"ACC_01JBC8Z5Q0M6T9X2V4R7K3N5PW",
		" acc_01JBC8Z5Q0M6T9X2V4R7K3N5PW",
		"usr_01JBC8Z5Q0M6T9X2V4R7K3N5PW",
	}
	for _, id := range invalid {
		if ValidTerraAccountID(id) {
			t.Errorf("%q: want invalid", id)
		}
	}
}

func TestValidIssuerURL(t *testing.T) {
	valid := []string{
		"https://id.terra.example",
		"https://id.terra.example:8443",
		"https://idp.corp.example/realms/terra",
		"http://localhost:8080",
		"http://127.0.0.1:9000",
		"http://[::1]:9000",
	}
	for _, value := range valid {
		if !ValidIssuerURL(value) {
			t.Errorf("%q: want valid", value)
		}
	}
	invalid := []string{
		"",
		"id.terra.example",
		"https://id.terra.example/",
		"https://id.terra.example?x=1",
		"https://id.terra.example?",
		"https://id.terra.example#frag",
		"https://user@id.terra.example",
		"http://id.terra.example",
		"http://10.0.0.1",
		"ftp://id.terra.example",
		"https://",
		" https://id.terra.example",
		"https:id.terra.example",
	}
	for _, value := range invalid {
		if ValidIssuerURL(value) {
			t.Errorf("%q: want invalid", value)
		}
	}
}

func TestTreeAudience(t *testing.T) {
	aud, err := TreeAudience(testTerraIDNodeID)
	if err != nil || aud != "tree:"+testTerraIDNodeID {
		t.Fatalf("TreeAudience = %q, %v", aud, err)
	}
	nodeID, err := ParseTreeAudience(aud)
	if err != nil || nodeID != testTerraIDNodeID {
		t.Fatalf("ParseTreeAudience = %q, %v", nodeID, err)
	}
	for _, bad := range []string{"", "lab-1", "node_", " " + testTerraIDNodeID, testTerraIDNodeID + " ", "node_a/b"} {
		if _, err := TreeAudience(bad); !errors.Is(err, ErrTreeAudienceInvalid) {
			t.Errorf("TreeAudience(%q): %v", bad, err)
		}
	}
	for _, bad := range []string{"", testTerraIDNodeID, "tree:", "tree:lab-1", "Tree:" + testTerraIDNodeID, "tree: " + testTerraIDNodeID} {
		if _, err := ParseTreeAudience(bad); !errors.Is(err, ErrTreeAudienceInvalid) {
			t.Errorf("ParseTreeAudience(%q): %v", bad, err)
		}
	}
}

func TestExternalExchangeContract(t *testing.T) {
	raw, _ := json.Marshal(ExternalExchangeRequest{Token: "a.b.c"})
	if string(raw) != `{"token":"a.b.c"}` {
		t.Fatalf("request shape %s", raw)
	}
	codes := map[string]string{
		ErrCodeExternalTokenInvalid:      "EXTERNAL_TOKEN_INVALID",
		ErrCodeExternalIdentityNotLinked: "EXTERNAL_IDENTITY_NOT_LINKED",
		ErrCodeExternalIdentitySuspended: "EXTERNAL_IDENTITY_SUSPENDED",
		ErrCodeExternalMFARequired:       "EXTERNAL_MFA_REQUIRED",
	}
	for got, want := range codes {
		if got != want {
			t.Errorf("error code %q, want %q", got, want)
		}
	}
}

func TestTerraIDParentPin(t *testing.T) {
	parent := "node_1787923476494_aa11bb22cc33dd44"
	root := ""
	bad := "desk"

	claims := testTerraIDClaims(t, TerraIDUseNodeEnrollCodeIssue)
	for _, pin := range []*string{nil, &parent, &root} {
		claims.ParentNodeID = pin
		if err := ValidateTerraIDClaims(claims, testTerraIDExpectation(t, TerraIDUseNodeEnrollCodeIssue)); err != nil {
			t.Errorf("pin %v rejected: %v", pin, err)
		}
	}
	claims.ParentNodeID = &bad
	if err := ValidateTerraIDClaims(claims, testTerraIDExpectation(t, TerraIDUseNodeEnrollCodeIssue)); !errors.Is(err, ErrTerraIDTokenInvalid) {
		t.Errorf("malformed pin: err = %v", err)
	}

	login := testTerraIDClaims(t, TerraIDUseTreeLogin)
	login.ParentNodeID = &parent
	if err := ValidateTerraIDClaims(login, testTerraIDExpectation(t, TerraIDUseTreeLogin)); !errors.Is(err, ErrTerraIDTokenInvalid) {
		t.Errorf("pin on a login token: err = %v", err)
	}

	// Present-and-empty must survive the wire: it means "the root", not "unpinned".
	claims.ParentNodeID = &root
	raw, _ := json.Marshal(claims)
	var back TerraIDClaims
	if err := json.Unmarshal(raw, &back); err != nil || back.ParentNodeID == nil || *back.ParentNodeID != "" {
		t.Errorf("root pin round trip = %v, %v (%s)", back.ParentNodeID, err, raw)
	}
}
