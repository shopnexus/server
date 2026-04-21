package response

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"runtime"
	"strings"

	sharedmodel "shopnexus-server/internal/shared/model"

	"github.com/bytedance/sonic"
	restate "github.com/restatedev/sdk-go"
)

const (
	ContentTypeJSON = "application/json"
)

// errorCodePattern matches a valid app-level error code identifier embedded
// as a message prefix. Snake_case, starts with a lowercase letter, 3–50 chars.
// This gates the cross-service extraction path so regular messages that happen
// to contain ": " (e.g. "failed to load item: not found") are not misparsed.
var errorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,49}$`)

func writeError(w http.ResponseWriter, httpCode int, err error) error {
	// Preserve the original sharedmodel.Error struct (including Code) when the
	// error originates same-process. Only fall back to reconstructing when the
	// error didn't come from this codebase's error type — which typically means
	// it crossed a Restate service boundary and lost the struct on the wire.
	var e sharedmodel.Error
	if domainErr, ok := errors.AsType[sharedmodel.Error](err); ok {
		e = domainErr
		httpCode = int(domainErr.HTTPStatus)
	} else {
		e = sharedmodel.NewError(uint16(httpCode), err.Error())
	}

	// Cross-service safety: when Code is empty but the message starts with a
	// valid identifier prefix ("wallet_not_empty: ..."), extract it back into
	// Code. This makes FE's `err.code === "wallet_not_empty"` branching work
	// uniformly whether the error was thrown locally or bubbled through
	// Restate from another service.
	if e.Code == "" && e.Message != "" {
		if before, _, ok := strings.Cut(e.Message, ": "); ok && errorCodePattern.MatchString(before) {
			e.Code = before
		}
	}

	data, marshalErr := sonic.Marshal(CommonResponse{
		Error: &e,
	})
	if marshalErr != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return marshalErr
	}

	w.Header().Set("Content-Type", ContentTypeJSON)
	w.WriteHeader(httpCode)
	_, writeErr := w.Write(data)
	return writeErr
}

func writeResponse(w http.ResponseWriter, httpCode int, dto any) error {
	data, err := MarshalJSONWithEmptyArrays(dto)
	if err != nil {
		return writeError(w, http.StatusInternalServerError, err)
	}

	w.Header().Set("Content-Type", ContentTypeJSON)
	w.WriteHeader(httpCode)
	_, writeErr := w.Write(data)
	return writeErr
}

// FromDTO writes a successful response with the provided DTO.
func FromDTO(w http.ResponseWriter, httpCode int, dto any) error {
	return writeResponse(w, httpCode, CommonResponse{
		Data: dto,
	})
}

// FromMessage writes a response with a string message as data.
func FromMessage(w http.ResponseWriter, httpCode int, message string) error {
	return writeResponse(w, httpCode, CommonResponse{
		Data: message,
	})
}

// FromError writes an error response based on the provided error type.
// If the error is a Restate terminal error with a specific code, that code takes precedence over httpCode.
func FromError(w http.ResponseWriter, httpCode int, err error) error {
	if err == nil {
		return FromDTO(w, http.StatusOK, nil)
	}

	// Extract code from Restate terminal errors (e.g. cross-module 409 propagated through ingress)
	if restate.IsTerminalError(err) {
		if code := int(restate.ErrorCode(err)); code >= 400 && code < 600 {
			httpCode = code
		}
	}

	slog.Error("HTTP error",
		slog.Int("http_code", httpCode),
		slog.Any("error", err),
		slog.String("stack", getStackTrace()),
	)

	return writeError(w, httpCode, err)
}

// FromHTTPCode writes a response based on the provided HTTP status code.
func FromHTTPCode(w http.ResponseWriter, httpCode int) error {
	if httpCode < 100 || httpCode > 599 {
		httpCode = http.StatusInternalServerError
	}

	statusText := http.StatusText(httpCode)
	if statusText == "" {
		statusText = "Unknown Error"
	}

	e := sharedmodel.NewError(uint16(httpCode), statusText)
	response := CommonResponse{
		Error: &e,
	}

	return writeResponse(w, httpCode, response)
}

// FromPaginate writes a paginated response.
func FromPaginate[T any](w http.ResponseWriter, paginate sharedmodel.PaginateResult[T]) error {
	data := paginate.Data
	if data == nil {
		data = make([]T, 0)
	}

	response := PaginationResponse[T]{
		Data: data,
		PageMeta: PageMeta{
			Limit:      paginate.PageParams.Limit,
			Total:      paginate.Total,
			Page:       paginate.PageParams.Page,
			Cursor:     paginate.PageParams.Cursor,
			NextPage:   paginate.NextPage(),
			NextCursor: paginate.EncodeNextCursor(),
		},
	}

	return writeResponse(w, http.StatusOK, response)
}

func getStackTrace() string {
	var pc [32]uintptr
	n := runtime.Callers(2, pc[:]) // skip runtime.Callers and getStackTrace itself

	var builder strings.Builder
	builder.WriteString("Stack trace:\n")

	frames := runtime.CallersFrames(pc[:n])
	for {
		frame, more := frames.Next()
		if !strings.Contains(frame.File, "runtime/") &&
			!strings.Contains(frame.File, "testing/") {
			fmt.Fprintf(&builder, "  at %s (%s:%d)\n",
				frame.Function, frame.File, frame.Line)
		}
		if !more {
			break
		}
	}
	return builder.String()
}
