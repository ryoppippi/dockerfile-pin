package compose

import "strings"

func RewriteFile(content string, refs []ComposeImageRef, digests map[int]string) string {
	lines := strings.Split(content, "\n")
	for i, ref := range refs {
		digest, ok := digests[i]
		if !ok || ref.Skip {
			continue
		}
		lineIdx := ref.Line - 1
		if lineIdx >= 0 && lineIdx < len(lines) {
			oldValue := ref.RawRef
			var newValue string
			if atIdx := strings.Index(oldValue, "@"); atIdx >= 0 {
				newValue = oldValue[:atIdx] + "@" + digest
			} else {
				newValue = oldValue + "@" + digest
			}
			lines[lineIdx] = strings.Replace(lines[lineIdx], oldValue, newValue, 1)
		}
	}
	return strings.Join(lines, "\n")
}
