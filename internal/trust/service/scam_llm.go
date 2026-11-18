package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopnexus/server/internal/provider/llm"
	"github.com/shopnexus/server/internal/trust/domain"
)

// ScamPromptVersion is stored with each check the LLM makes; raise it with
// every change of ScamSystemPrompt or scamSchema.
const ScamPromptVersion = 1

// ScamSystemPrompt tells the model what each message code means.
const ScamSystemPrompt = `You read a chat between two people, A and B, on ShopNexus, a Vietnamese marketplace for used goods. Buyers pay cash on delivery through the seller's own carrier account, or meet the seller in person. ShopNexus holds no money and never asks anyone to pay through a chat. Phone numbers, bank and card numbers, email addresses and links were replaced with <PHONE>, <BANK_NUMBER>, <EMAIL> and <URL:domain>.

For each of A and B, list the codes that person's own messages show. Judge a person only by what they wrote. List nothing for an ordinary chat about an item, its price, a meeting or a delivery: haggling, asking for photos, asking where to meet and giving a phone number to call at the meeting are ordinary.

Codes:
- prepayment_request: asks the other to transfer money or a deposit before receiving the item ("chuyển cọc trước", "ck trước 50%").
- off_app_contact: pushes the chat to Zalo, Telegram, Facebook or another app ("kết bạn zalo", "ib tele").
- suspicious_link: sends a link to pay, to track a parcel, to claim money or to log in.
- sensitive_info_request: asks for an OTP, a password, a card number, a CVV or a banking login.
- impersonation: claims to be ShopNexus staff, a bank, a carrier or an authority.
- fake_payment_proof: shows or mentions a transfer receipt to make the other ship first, or says they paid too much and asks for the difference back.
- pressure: pushes for a decision now with a deadline, scarcity or a threat ("chốt trong 10 phút", "nhiều người hỏi lắm").`

// scamSchema allows only the message codes, in the order of
// domain.MessageCodes; TestLLMClassifier holds the two together.
var scamSchema = []byte(`{"type":"object","additionalProperties":false,"required":["a","b"],"properties":{` +
	`"a":{"type":"array","items":{"type":"string","enum":["prepayment_request","off_app_contact","suspicious_link",` +
	`"sensitive_info_request","impersonation","fake_payment_proof","pressure"]}},` +
	`"b":{"type":"array","items":{"type":"string","enum":["prepayment_request","off_app_contact","suspicious_link",` +
	`"sensitive_info_request","impersonation","fake_payment_proof","pressure"]}}}}`)

// LLMClassifier asks a language model through provider/llm. Alias is the
// model's name at the gateway, scam-check in the cluster.
type LLMClassifier struct {
	Model   llm.Client
	Alias   string
	Timeout time.Duration
	Log     *slog.Logger
}

func (c LLMClassifier) Name() string     { return "llm:" + c.Alias }
func (LLMClassifier) PromptVersion() int { return ScamPromptVersion }

// Classify sends one line per message, "A: text" or "B: text", with the
// line breaks of a message turned into spaces, and logs the counts, codes
// and time of the answer without any text.
func (c LLMClassifier) Classify(ctx context.Context, w domain.Window) (domain.Verdict, error) {
	var b strings.Builder
	for _, l := range w.Lines {
		b.WriteString(strings.ToUpper(string(l.Side)))
		b.WriteString(": ")
		b.WriteString(strings.Join(strings.Fields(l.Text), " "))
		b.WriteString("\n")
	}
	started := time.Now()
	callCtx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	var v domain.Verdict
	err := c.Model.Complete(callCtx, llm.Request{
		System: ScamSystemPrompt, User: b.String(), SchemaName: "scam_check", Schema: scamSchema,
	}, &v)
	if errors.Is(err, llm.ErrRefused) {
		return domain.Verdict{}, fmt.Errorf("%w: %w", ErrRefused, err)
	}
	if err != nil {
		return domain.Verdict{}, fmt.Errorf("ask the model: %w", err)
	}
	c.Log.InfoContext(ctx, "scam check classified", "prompt_version", ScamPromptVersion, "lines", len(w.Lines),
		"chars", utf8.RuneCountInString(b.String()), "a", v.A, "b", v.B, "ms", time.Since(started).Milliseconds())
	return v, nil
}
