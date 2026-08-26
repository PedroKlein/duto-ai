package publisher

// NewVerifiedForTest builds a Verified with the given operations so black-box
// tests can exercise Publish orchestration without a full bundle. It exists
// only in test builds.
func NewVerifiedForTest(operationSet string, operations []Operation) *Verified {
	return &Verified{
		bundleSHA256: "bundle", planSHA256: "plan", policySHA256: "policy",
		repositoryID: "1001", operationSet: operationSet, operations: operations,
	}
}
