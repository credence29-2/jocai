package store

// Permission bits. They mirror Discord's meaning but not its numbering.
const (
	PermViewChannel     uint64 = 1 << 0
	PermSendMessages    uint64 = 1 << 1
	PermAttachFiles     uint64 = 1 << 2
	PermAddReactions    uint64 = 1 << 3
	PermMentionEveryone uint64 = 1 << 4
	PermManageMessages  uint64 = 1 << 5
	PermReadHistory     uint64 = 1 << 6
	PermConnect         uint64 = 1 << 7
	PermSpeak           uint64 = 1 << 8
	PermVideo           uint64 = 1 << 9 // webcam and screen share
	PermUseVAD          uint64 = 1 << 10
	PermMuteMembers     uint64 = 1 << 11
	PermDeafenMembers   uint64 = 1 << 12
	PermMoveMembers     uint64 = 1 << 13
	PermKickMembers     uint64 = 1 << 14
	PermBanMembers      uint64 = 1 << 15
	PermCreateInvite    uint64 = 1 << 16
	PermManageChannels  uint64 = 1 << 17
	PermManageRoles     uint64 = 1 << 18
	PermManageServer    uint64 = 1 << 19
	PermChangeNickname  uint64 = 1 << 20
	PermManageNicknames uint64 = 1 << 21
	PermPrioritySpeaker uint64 = 1 << 22
	PermAdministrator   uint64 = 1 << 30

	PermAll uint64 = 1<<31 - 1

	PermDefault = PermViewChannel | PermSendMessages | PermAttachFiles | PermAddReactions |
		PermReadHistory | PermConnect | PermSpeak | PermVideo | PermUseVAD |
		PermCreateInvite | PermChangeNickname
)

// Base is what a member may do server-wide, before channel overwrites.
func (g *Guild) Base(uid ID) uint64 {
	if uid == g.OwnerID {
		return PermAll
	}
	m := g.Members[uid]
	if m == nil {
		return 0
	}
	p := g.Roles[g.ID].Permissions
	for _, rid := range m.Roles {
		if r := g.Roles[rid]; r != nil {
			p |= r.Permissions
		}
	}
	if p&PermAdministrator != 0 {
		return PermAll
	}
	return p
}

// In is what a member may do in one channel of this server.
func (g *Guild) In(uid ID, c *Channel) uint64 {
	p := g.Base(uid)
	if p == PermAll || c == nil {
		return p
	}
	m := g.Members[uid]
	var allow, deny uint64
	for _, o := range c.Overwrites {
		if o.ID == g.ID {
			p = p&^o.Deny | o.Allow
			continue
		}
		for _, rid := range m.Roles {
			if o.ID == rid {
				allow |= o.Allow
				deny |= o.Deny
			}
		}
	}
	p = p&^deny | allow
	// Overwrites for one member apply last.
	for _, o := range c.Overwrites {
		if o.ID == uid {
			p = p&^o.Deny | o.Allow
		}
	}
	if p&PermViewChannel == 0 {
		return 0
	}
	if c.Type == "text" {
		p &^= PermConnect | PermSpeak | PermVideo | PermUseVAD | PermPrioritySpeaker
	}
	return p
}

// TopRole is the position of a member's highest role; the owner outranks
// everyone.
func (g *Guild) TopRole(uid ID) int {
	if uid == g.OwnerID {
		return 1 << 30
	}
	top := 0
	if m := g.Members[uid]; m != nil {
		for _, rid := range m.Roles {
			if r := g.Roles[rid]; r != nil && r.Position > top {
				top = r.Position
			}
		}
	}
	return top
}

// Outranks reports whether actor may moderate target.
func (g *Guild) Outranks(actor, target ID) bool {
	if target == g.OwnerID {
		return false
	}
	return actor == g.OwnerID || g.TopRole(actor) > g.TopRole(target)
}
