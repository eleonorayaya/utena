package branchpicker

type branchItem struct {
	name   string
	remote bool
}

func (i branchItem) Title() string { return i.name }
func (i branchItem) Description() string {
	if i.remote {
		return "remote"
	}
	return ""
}
func (i branchItem) FilterValue() string { return i.name }
