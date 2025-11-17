package token

// pre condition: d is input bytes up until but not including comment lead token '#'
// indent is the indent of the current line (number of spaces)
// returns length of the whitespace prefix to include with the comment
func commentPrefix(d []byte, indent int) int {
	i := len(d) - 1
	startLn := -1
	startSp := -1
	for i >= 0 {
		switch d[i] {
		case ' ', '\t':
		case '\n':
			startLn = i + 1
			if startSp == -1 {
				startSp = i + 1
			}
			goto done
		case '-':
			if startSp != -1 {
				break
			}
			if i+1 < len(d) && d[i+1] == ' ' {
				startSp = i + 2
			}
		default:
			if startSp == -1 {
				startSp = i + 1
			}
		}
		i--
	}
done:
	if startSp == -1 {
		return 0
	}
	if startLn == -1 {
		// first line in d
		return len(d) - startSp
	}
	if startLn == len(d)-1 {
		return 0
	}
	off := max(startSp, startLn+indent)
	return len(d) - off
}
