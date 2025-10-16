package validator

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"

	sharedmodel "shopnexus-remastered/internal/module/shared/model"

	"github.com/go-playground/locales/en"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	entranslations "github.com/go-playground/validator/v10/translations/en"
	"github.com/guregu/null/v6"
)

var (
	once     sync.Once
	validate *CustomValidator
)

type CustomValidator struct {
	uni       *ut.UniversalTranslator
	validator *validator.Validate
}

func New() (*CustomValidator, error) {
	en := en.New()
	uni := ut.New(en, en)
	validate := validator.New(
		validator.WithRequiredStructEnabled(),
	)

	// Register default translations (en)
	trans, _ := uni.GetTranslator("en")
	if err := entranslations.RegisterDefaultTranslations(validate, trans); err != nil {
		return nil, fmt.Errorf("failed to register translations: %w", err)
	}

	// Register all null.* types to use the ValidateNullable CustomTypeFunc
	validate.RegisterCustomTypeFunc(
		ValidateNullable,
		null.Bool{},
		null.Byte{},
		null.Float{},
		null.Int16{},
		null.Int32{},
		null.Int64{},
		null.String{},
		null.Time{},
	)

	return &CustomValidator{
		uni:       uni,
		validator: validate,
	}, nil
}

func (cv *CustomValidator) Validate(i interface{}) error {
	err := cv.validator.Struct(i)
	if valErr, ok := err.(validator.ValidationErrors); ok {
		trans, _ := cv.uni.GetTranslator("en")
		text, err := json.Marshal(valErr.Translate(trans))
		if err != nil {
			// Fallback to the original validation error if JSON marshaling fails
			return valErr
		}

		return sharedmodel.NewError("validation", string(text))
	}

	return err
}

type NullType interface {
	IsZero() bool
	driver.Valuer
}

// Workaround for omitnil not working with "untyped nil"
// https://github.com/go-playground/validator/issues/1209#issuecomment-1892359649
var nilValue *struct{}

// ValidateNullable implements validator.CustomTypeFunc
func ValidateNullable(field reflect.Value) interface{} {
	if nullValue, ok := field.Interface().(NullType); ok {
		if nullValue.IsZero() {
			return nilValue // The "omitnil" validator work only with typed nil values
		}
		if val, err := nullValue.Value(); err == nil {
			return val
		}
	}
	return nil
}

// Export shortcut to get the singleton validator instance
func Validate(i any) error {
	once.Do(func() {
		var err error
		validate, err = New()
		if err != nil {
			panic(fmt.Sprintf("failed to create validator: %v", err))
		}
	})
	return validate.Validate(i)
}
