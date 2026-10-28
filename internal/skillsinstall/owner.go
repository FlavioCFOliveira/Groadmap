package skillsinstall

// ownerReasonFor is the decision of ownerReason for a known owner uid. It is
// platform independent, so the acceptance of root is tested on every host.
func ownerReasonFor(uid uint32, path string, euid int, acceptRoot bool) string {
	if euid >= 0 && uint64(uid) == uint64(euid) {
		return ""
	}
	if acceptRoot {
		if uid == 0 {
			return ""
		}
		return path + " is owned neither by root nor by the invoking user"
	}
	return path + " is not owned by the invoking user"
}
