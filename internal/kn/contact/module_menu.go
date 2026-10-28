package contact

import _ "embed"

//go:embed menus/base.json
var menuSource []byte

// MenuSource trả bản sao nguồn menu WH-A của module (KNSPLIT-2-MENU-KN) để gốc lắp ráp chuyển vào
// extension.MenuCatalog — module giữ nguồn của mình, không bản sao JSON thứ hai.
func MenuSource() []byte { return append([]byte(nil), menuSource...) }
