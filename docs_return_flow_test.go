package stripe

import (
	"regexp"
	"strings"
	"testing"
)

func TestStripeDocs_UseOpaqueReturnRefsForCanonicalVerification(t *testing.T) {
	spec := readDocFile(t, "STRIPE_INTEGRATION.md")
	guide := readDocFile(t, "DEVELOPER_GUIDE.md")

	requiredPatterns := []*regexp.Regexp{
		regexp.MustCompile(`type PaymentIntentSession struct \{\s*PaymentIntent \*stripelib\.PaymentIntent\s*ReturnRef\s+string`),
		regexp.MustCompile(`type VerifyPaymentIntentReturnParams struct \{\s*ReturnRef string\s*OwnerKey\s+string`),
		regexp.MustCompile(`VerifyPaymentIntentReturn\(`),
		regexp.MustCompile(`ctx\.Navigate\("/checkout/complete\?ref="\+url\.QueryEscape\(returnRef\), vango\.WithReplace\(\)\)`),
	}

	for _, pattern := range requiredPatterns {
		if !pattern.MatchString(spec) {
			t.Fatalf("spec missing canonical return-flow pattern %q", pattern.String())
		}
		if !pattern.MatchString(guide) {
			t.Fatalf("developer guide missing canonical return-flow pattern %q", pattern.String())
		}
	}
}

func TestStripeDocs_DoNotUseURLSuppliedPaymentIntentIDsForCanonicalReturnVerification(t *testing.T) {
	files := []string{
		"STRIPE_INTEGRATION.md",
		"DEVELOPER_GUIDE.md",
	}

	forbiddenPatterns := []*regexp.Regexp{
		regexp.MustCompile(`type CheckoutCompleteProps struct \{\s*PaymentIntentID string`),
		regexp.MustCompile(`Read ` + "`payment_intent`" + ` from the return URL`),
		regexp.MustCompile(`return routes\.GetDeps\(\)\.Payments\.GetPaymentIntent\(ctx, id\)`),
		regexp.MustCompile(`paymentIntentID := setup\.URLParam\(&s, "payment_intent"`),
	}

	for _, name := range files {
		content := readDocFile(t, name)
		for _, pattern := range forbiddenPatterns {
			if pattern.MatchString(content) {
				t.Fatalf("%s reintroduced unsafe canonical return-flow pattern %q", name, pattern.String())
			}
		}
	}
}

func TestStripeDocs_DescribeReturnURLScrubbingAndOwnerBinding(t *testing.T) {
	files := []string{
		"STRIPE_INTEGRATION.md",
		"DEVELOPER_GUIDE.md",
		"README.md",
	}

	for _, name := range files {
		content := readDocFile(t, name)
		required := []string{
			"ref",
			"payment_intent_client_secret",
			"server-side",
		}
		for _, fragment := range required {
			if !strings.Contains(content, fragment) {
				t.Fatalf("%s missing return-flow fragment %q", name, fragment)
			}
		}
	}

	README := readDocFile(t, "README.md")
	if !strings.Contains(README, "Do not call Stripe with a `payment_intent=pi_...` read directly from the browser URL.") {
		t.Fatalf("README must explicitly forbid using payment_intent from the browser URL as an authorization key")
	}
}
