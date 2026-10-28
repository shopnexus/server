package telemetry

// Must returns the instrument a meter created and panics on the error that
// came with it. Instruments are created once at startup, where the only
// error is a malformed name or option: a mistake in the code.
//
//	logins = telemetry.Must(meter.Int64Counter("login_total", metric.WithDescription("sign-in attempts")))
func Must[T any](instrument T, err error) T {
	if err != nil {
		panic(err)
	}
	return instrument
}
