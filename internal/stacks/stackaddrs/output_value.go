package stackaddrs

type OutputValue struct {
	Name string
}

func (OutputValue) inStackConfigSigil()   {}
func (OutputValue) inStackInstanceSigil() {}

func (v OutputValue) String() string {
	return "output." + v.Name
}

// ConfigOutputValue places an [OutputValue] in the context of a particular [Stack].
type ConfigOutputValue = InStackConfig[OutputValue]

// AbsOutputValue places an [OutputValue] in the context of a particular [StackInstance].
type AbsOutputValue = InStackInstance[OutputValue]
