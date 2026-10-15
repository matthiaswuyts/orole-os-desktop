package sandbox

// DefaultPins is the QEMU build and guest image this Orole build downloads. It stays empty until the
// qemu-v* and guest-v* releases are published; scripts/sandbox-pins.ps1 then writes the values.
// With empty pins the runtime reports "runtime-missing" and Install returns ErrNoPins.
var DefaultPins = Pins{}
