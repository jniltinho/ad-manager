// Command ad-manager manages an Active Directory and supports the
// migration of the Windows 2008 domain controller to Samba 4. It delegates to
// cmd.Execute (Cobra).
package main

import "ad-manager/cmd"

func main() {
	cmd.Execute()
}
