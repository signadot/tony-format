package token

func number(d []byte) (int, bool, error) {
	digits := asciiDigits(d)
	if digits == 0 {
		return 0, false, ErrNumber
	}
	f := fract(d[digits:])
	e := exp(d[digits+f:])
	if f+e == 0 {
		if digits > 1 && d[0] == '0' {
			return digits, false, ErrNumberLeadingZero
		}
		return digits, false, nil
	}
	return f + e + digits, true, nil
}

func asciiDigits(d []byte) int {
	i := 0
	for i < len(d) {
		if !asciiDigit(d[i]) {
			return i
		}
		i++
	}
	return i
}

func asciiDigit(c byte) bool {
	switch c {
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return true
	default:
		return false
	}
}

func exp(d []byte) int {
	if len(d) < 2 {
		return 0
	}
	switch d[0] {
	case 'e', 'E':
	default:
		return 0
	}
	i := 1
	switch d[1] {
	case '+', '-':
		i++
	default:
	}
	if i == len(d) {
		return 0
	}
	n := asciiDigits(d[i:])
	if n == 0 {
		return 0
	}
	return n + i
}

func fract(d []byte) int {
	if len(d) == 0 {
		return 0
	}
	if d[0] != '.' {
		return 0
	}
	for i := 1; i < len(d); i++ {
		if !asciiDigit(d[i]) {
			if i == 1 {
				// . must be followed by 1 or more digits rfc 7159
				return 0
			}
			return i
		}
	}
	return 0
}
