package sourceestimate

type fieldRef struct {
	owner string
	field gradedSurfaceField
	unit  int
}

type consumerRecord struct {
	constraint gradedConstraint
	refs       []fieldRef
	caller     *operation
	lifecycle  bool
}
