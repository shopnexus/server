package telemetry

// Must returns the instrument a meter created and panics on the error that
// came with it. Instruments are created once at startup, where the only
// error is a malformed name or option: a mistake in the code.
//
//	otpSent = telemetry.Must(meter.Int64Counter("otp_sent_total", metric.WithDescription("OTP codes sent")))
func Must[T any](instrument T, err error) T {
	if err != nil {
		panic(err)
	}
	return instrument
}
