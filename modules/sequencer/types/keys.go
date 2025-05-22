package types

// ModuleName is the name of the sequencer module
const ModuleName = "sequencer"

var (
	SequencerConsAddrKey      = []byte{0x11}
	LastValidatorSetKey       = []byte{0x12}
	NextSequencerChangeHeight = []byte{0x13}
	ParamsKey                 = []byte{0x14}
)
