package scene

// Extend lengthens a demo to newFrames by CONTINUATION: the original frames stay exactly
// as Konami wrote them, and the new tail continues whatever was moving.
//
// The rule is one thing applied everywhere: extrapolate each channel by the velocity it
// carried over its last real frames. A channel that was translating (Arsenal sailing -Z,
// the wake control points riding with it) keeps translating; a channel that was static
// (a light colour) holds. The camera is the deliberate exception -- its fly-around must
// not fly on forever, so it holds at the last frame as a placeholder for the shot we
// author next.
//
// Per-frame effects (the wake, the lights) are carried into the new tail via Continue, so
// they keep firing past their base frames instead of dying at the seam.
func (s *Scene) Extend(newFrames int) {
	old := s.Frames
	if newFrames <= old {
		return
	}

	// Camera: hold. It is a placeholder until the approach shot is keyed. Do NOT mark it
	// dirty -- the extended frames regenerate the camera from these held samples (their base
	// is empty), while the original frames must pass through byte-exact.
	c := s.Camera
	for _, ch := range []*Channel{&c.PosX, &c.PosY, &c.PosZ, &c.TrgX, &c.TrgY, &c.TrgZ, &c.Screen, &c.Roll} {
		holdChannel(ch, old, newFrames)
		ch.Dirty = false
	}

	// Objects: the ship and its rig keep sailing. Extrapolate position, hold rotation, and
	// hold the last joint pose (rigid props have none to speak of). Only objects still
	// animated at the old end continue -- the crash demo has three that exist only in frame 0
	// and must be left exactly where they were.
	for _, o := range s.Objects {
		if !o.MotionOK || len(o.Joints) < old || o.Joints[old-1] == nil {
			continue
		}
		extrapChannel(&o.PosX, old, newFrames)
		extrapChannel(&o.PosY, old, newFrames)
		extrapChannel(&o.PosZ, old, newFrames)
		holdChannel(&o.RotX, old, newFrames)
		holdChannel(&o.RotY, old, newFrames)
		holdChannel(&o.RotZ, old, newFrames)
		if len(o.Joints) > 0 {
			last := lastJoints(o, old)
			for len(o.Joints) < newFrames {
				o.Joints = append(o.Joints, last)
			}
		}
	}
	// No MotionDirty: the baker re-encodes only the extended frames (the `extended` flag),
	// so the original half's motion packs pass through byte-exact.

	// Per-frame effects (exec 0) that ran to the old end are carried on. Their positional
	// params extrapolate with the ship; their colours hold. Exec-1 one-shots have long
	// expired, so they are left alone. Not marked Dirty -- the original frames pass through,
	// and Continue makes the extended frames re-emit from the extrapolated params.
	for _, e := range s.Effects {
		if e.ExecType != 0 || e.Deleted || e.End < old {
			continue
		}
		for _, ch := range e.Params {
			extrapChannel(ch, old, newFrames)
		}
		e.End = newFrames
		e.Continue = true
	}

	s.Frames = newFrames
}

// holdChannel repeats the last real sample across the new range.
func holdChannel(ch *Channel, old, n int) {
	if len(ch.Baked) < old {
		return
	}
	last := 0.0
	if old > 0 {
		last = ch.Baked[old-1]
	}
	for len(ch.Baked) < n {
		ch.Baked = append(ch.Baked, last)
	}
}

// extrapChannel continues the channel at the velocity of its last real frames. A window of
// 60 frames (1 s) smooths out per-frame jitter without lagging a real move.
func extrapChannel(ch *Channel, old, n int) {
	if len(ch.Baked) < old || old < 2 {
		holdChannel(ch, old, n)
		if len(ch.Baked) >= old {
			ch.Dirty = true
		}
		return
	}
	win := 60
	if win > old-1 {
		win = old - 1
	}
	last := ch.Baked[old-1]
	vel := (last - ch.Baked[old-1-win]) / float64(win)
	for i := 0; len(ch.Baked) < n; i++ {
		ch.Baked = append(ch.Baked, last+vel*float64(i+1))
	}
	ch.Dirty = true
}

func lastJoints(o *ObjectTrack, old int) []Joint {
	for i := min(old, len(o.Joints)) - 1; i >= 0; i-- {
		if o.Joints[i] != nil {
			return o.Joints[i]
		}
	}
	return nil
}
