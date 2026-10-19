package ad

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/go-ldap/ldap/v3"
)

// GPO is a Group Policy Object as the directory keeps it (the container half; the files are in SYSVOL).
type GPO struct {
	GUID           string    `json:"guid"` // {…}
	DN             string    `json:"dn"`
	Name           string    `json:"name"`
	UserEnabled    bool      `json:"user_enabled"`    // flags bit 1 off
	MachineEnabled bool      `json:"machine_enabled"` // flags bit 2 off
	UserVersion    int       `json:"user_version"`    // versionNumber high 16 bits
	MachineVersion int       `json:"machine_version"` // versionNumber low 16 bits
	Path           string    `json:"path"`            // gPCFileSysPath (SYSVOL)
	Extensions     []string  `json:"extensions"`      // client-side extensions, friendly names where known
	Links          []GPOLink `json:"links"`
}

// GPOLink is where a GPO applies: a container (domain, OU, site) with the link's options.
type GPOLink struct {
	Target   string `json:"target"` // DN of the domain, OU or site
	Enforced bool   `json:"enforced"`
	Disabled bool   `json:"disabled"`
	Order    int    `json:"order"` // link order of the container: 1 has the highest precedence (applied last)
}

// GPOContainer is a container that has GPO links or blocks inheritance.
type GPOContainer struct {
	DN               string `json:"dn"`
	BlockInheritance bool   `json:"block_inheritance"` // gPOptions = 1
	Links            int    `json:"links"`
}

// gpoExtensions names the client-side extensions most GPOs use (GUID of gPC*ExtensionNames).
var gpoExtensions = map[string]string{
	"{35378EAC-683F-11D2-A89A-00C04FBBCFA2}": "Registry",
	"{827D319E-6EAC-11D2-A4EA-00C04F79F83A}": "Security",
	"{42B5FAAE-6536-11D2-AE5A-0000F87571E3}": "Scripts",
	"{C6DC5466-785A-11D2-84D0-00C04FB169F7}": "Software Installation",
	"{25537BA6-77A8-11D2-9B6C-0000F8080861}": "Folder Redirection",
	"{B1BE8D72-6EAC-11D2-A4EA-00C04F79F83A}": "EFS Recovery",
	"{F3CCC681-B74C-4060-9F26-CD84525DCA2A}": "Audit Policy",
	"{5794DAFD-BE60-433F-88A2-1A31939AC01F}": "Drive Maps (Preferences)",
	"{17D89FEC-5C44-4972-B12D-241CAEF74509}": "Local Users and Groups (Preferences)",
	"{A3F3E39B-5D83-4940-B954-28315B82F0A8}": "Folder Options (Preferences)",
	"{BC75B1ED-5833-4858-9BB8-CBF0B166DF9D}": "Printers (Preferences)",
	"{B087BE9D-ED37-454F-AF9C-04291E351182}": "Registry (Preferences)",
	"{7150F9BF-48AD-4DA4-A49C-29EF4A8369BA}": "Files (Preferences)",
	"{6232C319-91AC-4931-9385-E70C2B099F0E}": "Folders (Preferences)",
	"{AADCED64-746C-4633-A97C-D61349046527}": "Scheduled Tasks (Preferences)",
	"{91FBB303-0CD5-4055-BF42-E512A681B325}": "Services (Preferences)",
	"{E437BC1C-AA7D-11D2-A382-00C04F991E27}": "IP Security",
	"{0ACDD40C-75AC-47AB-BAA0-BF6DE7E7FE63}": "Wireless",
	"{D02B1F72-3407-48AE-BA88-E8213C6761F1}": "Administrative Templates",
}

// extGUID is one GUID inside gPC*ExtensionNames ("[{CSE}{tool}…][…]": the first of each bracket is the CSE).
var extGUID = regexp.MustCompile(`\[(\{[0-9A-Fa-f-]{36}\})`)

// ParseGPLink splits a gPLink value ("[LDAP://cn={GUID},…;options]…") into its links. The last one stored has the
// highest precedence: it is link order 1, as the Group Policy console numbers them.
func ParseGPLink(v string) ([]GPOLink, error) {
	var out []GPOLink
	for _, part := range strings.Split(v, "]") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		dn, opt, ok := strings.Cut(part, ";")
		if !ok || !strings.HasPrefix(strings.ToUpper(dn), "[LDAP://") {
			return nil, fmt.Errorf("%w: gPLink %q", ErrWrongInput, part)
		}
		o, err := strconv.Atoi(strings.TrimSpace(opt))
		if err != nil {
			return nil, fmt.Errorf("%w: gPLink %q", ErrWrongInput, part)
		}
		out = append(out, GPOLink{Target: dn[len("[LDAP://"):], Disabled: o&1 != 0, Enforced: o&2 != 0})
	}
	for i := range out {
		out[i].Order = len(out) - i
	}
	return out, nil
}

// GPOs lists the domain's GPOs with their links (domain, OUs and sites), and the containers that link GPOs or
// block inheritance. Read only.
func (c *Conn) GPOs() ([]GPO, []GPOContainer, error) {
	base := c.cfg.Base()
	es, err := c.search("CN=Policies,CN=System,"+base, ldap.ScopeSingleLevel, "(objectClass=groupPolicyContainer)",
		[]string{"cn", "displayName", "flags", "versionNumber", "gPCFileSysPath", "gPCMachineExtensionNames", "gPCUserExtensionNames"})
	if err != nil {
		return nil, nil, err
	}
	gpos := make([]GPO, 0, len(es))
	byDN := map[string]int{}
	for _, e := range es {
		flags, _ := strconv.Atoi(e.GetAttributeValue("flags"))
		v, _ := strconv.ParseInt(e.GetAttributeValue("versionNumber"), 10, 64)
		ver := uint32(v) // Int32 in LDAP: a large user version comes negative
		var exts []string
		for _, m := range extGUID.FindAllStringSubmatch(e.GetAttributeValue("gPCMachineExtensionNames")+e.GetAttributeValue("gPCUserExtensionNames"), -1) {
			name := gpoExtensions[strings.ToUpper(m[1])]
			if name == "" {
				name = strings.ToUpper(m[1])
			}
			if !slices.Contains(exts, name) {
				exts = append(exts, name)
			}
		}
		slices.Sort(exts)
		g := GPO{GUID: strings.ToUpper(e.GetAttributeValue("cn")), DN: e.DN, Name: e.GetAttributeValue("displayName"),
			UserEnabled: flags&1 == 0, MachineEnabled: flags&2 == 0, UserVersion: int(ver >> 16), MachineVersion: int(ver & 0xffff),
			Path: e.GetAttributeValue("gPCFileSysPath"), Extensions: exts, Links: []GPOLink{}}
		if g.Extensions == nil {
			g.Extensions = []string{}
		}
		byDN[strings.ToLower(e.DN)] = len(gpos)
		gpos = append(gpos, g)
	}
	// the domain, its OUs and the sites (in the forest's configuration): gPLink (links) and gPOptions (blocked
	// inheritance)
	root, err := c.search("", ldap.ScopeBaseObject, "(objectClass=*)", []string{"configurationNamingContext"})
	if err != nil || len(root) == 0 {
		return nil, nil, fmt.Errorf("rootDSE: %w", err)
	}
	var conts []GPOContainer
	for _, s := range []struct {
		base, filter string
	}{{base, "(&(|(objectClass=domainDNS)(objectClass=organizationalUnit))(|(gPLink=*)(gPOptions=1)))"},
		{"CN=Sites," + root[0].GetAttributeValue("configurationNamingContext"), "(&(objectClass=site)(gPLink=*))"}} {
		cs, err := c.search(s.base, ldap.ScopeWholeSubtree, s.filter, []string{"gPLink", "gPOptions"})
		if err != nil {
			return nil, nil, err
		}
		for _, e := range cs {
			links, err := ParseGPLink(e.GetAttributeValue("gPLink"))
			if err != nil {
				return nil, nil, err
			}
			conts = append(conts, GPOContainer{DN: e.DN, BlockInheritance: e.GetAttributeValue("gPOptions") == "1", Links: len(links)})
			for _, l := range links {
				if i, ok := byDN[strings.ToLower(l.Target)]; ok {
					gpos[i].Links = append(gpos[i].Links, GPOLink{Target: e.DN, Enforced: l.Enforced, Disabled: l.Disabled, Order: l.Order})
				}
			}
		}
	}
	slices.SortFunc(gpos, func(a, b GPO) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	slices.SortFunc(conts, func(a, b GPOContainer) int { return strings.Compare(a.DN, b.DN) })
	return gpos, conts, nil
}
