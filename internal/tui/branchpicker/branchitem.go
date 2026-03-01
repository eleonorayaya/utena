package branchpicker

type branchItem struct {
	name string
}

func (i branchItem) Title() string       { return i.name }
func (i branchItem) Description() string { return "" }
func (i branchItem) FilterValue() string { return i.name }
