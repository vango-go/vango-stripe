package stripe

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestStripeDocs_UsePlanKeysInCanonicalBillingShapes(t *testing.T) {
	spec := readDocFile(t, "STRIPE_INTEGRATION.md")

	requiredPatterns := []*regexp.Regexp{
		regexp.MustCompile(`type SubscriptionParams struct \{\s*CustomerID string\s*PlanKey\s+pricing\.PlanKey`),
		regexp.MustCompile(`type CheckoutSessionParams struct \{\s*Mode\s+CheckoutMode\s*PlanKey\s+pricing\.PlanKey`),
		regexp.MustCompile(`CreateCheckoutSessionFromCheckout\(`),
	}

	for _, pattern := range requiredPatterns {
		if !pattern.MatchString(spec) {
			t.Fatalf("spec missing canonical pricing-boundary pattern %q", pattern.String())
		}
	}
}

func TestStripeDocs_DoNotUseRawStripePriceIDsAsClientFacingInputs(t *testing.T) {
	spec := readDocFile(t, "STRIPE_INTEGRATION.md")

	forbiddenPatterns := []*regexp.Regexp{
		regexp.MustCompile(`type SubscriptionParams struct \{\s*CustomerID string\s*PriceID\s+string`),
		regexp.MustCompile(`type CheckoutSessionParams struct \{\s*Mode\s+[^\n]+\s*PriceID\s+string`),
		regexp.MustCompile(`checkout\.Run\("price_`),
	}

	for _, pattern := range forbiddenPatterns {
		if pattern.MatchString(spec) {
			t.Fatalf("spec reintroduced raw Stripe price ID input pattern %q", pattern.String())
		}
	}
}

func TestStripeDocs_DescribeServerOwnedPricingBoundary(t *testing.T) {
	files := []string{
		"STRIPE_INTEGRATION.md",
		"DEVELOPER_GUIDE.md",
		"README.md",
	}

	for _, name := range files {
		content := readDocFile(t, name)
		required := []string{
			"PlanKey",
			"server-owned",
		}
		for _, fragment := range required {
			if !strings.Contains(content, fragment) {
				t.Fatalf("%s missing pricing-boundary fragment %q", name, fragment)
			}
		}
	}

	guide := readDocFile(t, "DEVELOPER_GUIDE.md")
	if !strings.Contains(guide, "opaque `checkoutID`") {
		t.Fatalf("developer guide must document opaque checkout IDs for variable-cart flows")
	}
}

func readDocFile(t *testing.T, name string) string {
	t.Helper()

	path := filepath.Join(".", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}
