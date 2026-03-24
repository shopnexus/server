package response

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"strings"

	commonmodel "shopnexus-server/internal/shared/model"

	"github.com/bytedance/sonic"
)

const (
	ContentTypeJSON = "application/json"
)

// writeError writes an error response with proper error handling
func writeError(w http.ResponseWriter, httpCode int, err error) error {
	// Default code and message
	errCode := uint16(httpCode)
	message := http.StatusText(httpCode)

	// Use the error's code and message if it's a domain error
	if domainErr, ok := errors.AsType[commonmodel.Error](err); ok {
		errCode = domainErr.Code()
		httpCode = int(errCode)
		message = domainErr.Error()
	}
	// debug.PrintStack()
	// traceError(err)
	GetStackTrace()

	data, err := sonic.Marshal(CommonResponse{
		Data: nil,
		Error: &commonmodel.Error{
			ErrCode: errCode,
			Message: message,
		},
	})
	if err != nil {
		// Fallback to plain text if JSON marshaling fails
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return err
	}

	w.Header().Set("Content-Type", ContentTypeJSON)
	w.WriteHeader(httpCode)
	_, writeErr := w.Write(data)
	return writeErr
}

// writeResponse is the core response writer with better error handling
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

// FromDTO writes a successful response with the provided DTO
func FromDTO(w http.ResponseWriter, httpCode int, dto any) error {
	return writeResponse(w, httpCode, CommonResponse{
		Data: dto,
	})
}

func FromMessage(w http.ResponseWriter, httpCode int, message string) error {
	return writeResponse(w, httpCode, CommonResponse{
		Data: message,
	})
}

// FromError writes an error response based on the provided error type
func FromError(w http.ResponseWriter, httpCode int, err error) error {
	fmt.Println(GetStackTrace())
	slog.Error("HTTP error", slog.Int("http_code", httpCode), slog.Any("error", err))
	if err == nil {
		return FromDTO(w, http.StatusOK, nil)
	}

	return writeError(w, httpCode, err)
}

// FromHTTPCode writes a response based on the provided HTTP status code
func FromHTTPCode(w http.ResponseWriter, httpCode int) error {
	// Validate HTTP status code
	if httpCode < 100 || httpCode > 599 {
		httpCode = http.StatusInternalServerError
	}

	statusText := http.StatusText(httpCode)

	// Use generic message if status text is empty
	if statusText == "" {
		statusText = "Unknown Error"
	}

	response := CommonResponse{
		Data:  nil,
		Error: &commonmodel.Error{ErrCode: uint16(httpCode), Message: statusText},
	}

	return writeResponse(w, httpCode, response)
}

// FromPaginate writes a paginated response with proper structure
func FromPaginate[T any](w http.ResponseWriter, paginate commonmodel.PaginateResult[T]) error {
	data := paginate.Data
	if data == nil {
		// Make sure the paginate object is not nil
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

func GetStackTrace() string {
	var pc [32]uintptr
	n := runtime.Callers(0, pc[:]) // 0 includes all frames

	var builder strings.Builder
	builder.WriteString("Stack trace:\n")

	frames := runtime.CallersFrames(pc[:n])
	for {
		frame, more := frames.Next()
		// Skip runtime and testing frames (like JavaScript skips built-ins)
		if !strings.Contains(frame.File, "runtime/") &&
			!strings.Contains(frame.File, "testing/") {
			builder.WriteString(fmt.Sprintf("  at %s (%s:%d)\n",
				frame.Function, frame.File, frame.Line))
		}
		if !more {
			break
		}
	}
	return builder.String()
}
