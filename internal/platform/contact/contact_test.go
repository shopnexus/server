package contact_test

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/shopnexus/server2/internal/platform/contact"
)

func TestParsePhone(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"0901234567", "+84901234567", "84901234567", "+84 90 123 4567", "090.123.4567", "090-123-4567", " 0901234567 ",
		"+84 090 123 4567", "+840901234567", "(090) 123-4567", "(+84) 90 123 4567",
	} {
		a, err := contact.ParsePhone(in)
		if err != nil || a.Value() != "+84901234567" || a.Kind() != contact.Phone {
			t.Errorf("%q → %q %v", in, a.Value(), err)
		}
	}
}

func TestParsePhoneRejects(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"", "0201234567", "090123456", "09012345678", "+14155550100", "+84901234567x", "+84 1234", "phone",
		"+8400901234567", "((090)) 123 4567x",
	} {
		if _, err := contact.ParsePhone(in); !errors.Is(err, contact.ErrInvalidPhone) {
			t.Errorf("%q: %v", in, err)
		}
	}
}

func TestParseEmail(t *testing.T) {
	t.Parallel()
	if a, err := contact.ParseEmail("  Linh.Nguyen@Gmail.COM "); err != nil || a.Value() != "linh.nguyen@gmail.com" || a.Kind() != contact.Email {
		t.Fatalf("%q %v", a.Value(), err)
	}
	for _, in := range []string{
		"", "linh", "linh@", "@gmail.com", "Linh <linh@gmail.com>", "linh@gmail", "linh@@gmail.com", "linh@.com",
		strings.Repeat("a", 250) + "@x.com",
	} {
		if _, err := contact.ParseEmail(in); !errors.Is(err, contact.ErrInvalidEmail) {
			t.Errorf("%q: %v", in, err)
		}
	}
}

func TestMaskingAndLogs(t *testing.T) {
	t.Parallel()
	phone, _ := contact.ParsePhone("0901234567")
	email, _ := contact.ParseEmail("linh@gmail.com")
	if phone.Masked() != "+8490****567" || email.Masked() != "l***@gmail.com" {
		t.Fatalf("%q %q", phone.Masked(), email.Masked())
	}
	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("x", "to", phone)
	if strings.Contains(buf.String(), "1234567") || !strings.Contains(buf.String(), "+8490****567") {
		t.Fatalf("log %q", buf.String())
	}
	var zero contact.Address
	if !zero.IsZero() || phone.IsZero() {
		t.Fatal("IsZero")
	}
}
