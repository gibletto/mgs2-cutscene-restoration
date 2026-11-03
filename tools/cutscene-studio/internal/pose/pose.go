// Package pose maps a demo's per-frame joint stream onto a model's skeleton.
//
// The engine's poser is obj_act.c:376-518 (SetMotionKms / MakeMatrixFromSkeleton). Both formats
// reduce to the same thing once you unpick the scratchpad arithmetic:
//
//	bone[i].rotation = the frame's quaternion for joint i, RELATIVE TO THE PARENT joint
//	bone[i].position = the joint's rest offset from its parent + the frame's joint translation
//	bone[-1]         = the object's world matrix (parent -1 indexes &Scrpad.world, which sits
//	                   immediately before matrix[0] -- the "parent = -1" trick, obj_act.c:496)
//
// EVM looks different in the source only because EVM_SKEL.rt is the offset from the ROOT, not the
// parent (as fmt_kmd.h:259 documents it), and the extra `mats.T = p + R*(-rt)` step is
// nothing but the inverse bind matrix folded into the joint matrix. Fold it back out and the
// recursion collapses to (rt[i] - rt[parent]) == the parent-relative offset, i.e. KMS's formula.
// That is why one Pose() serves both, and why the glTF's translation-only inverse bind (-Rest)
// is exactly right.
//
// Joint i of the demo drives joint i of the model. Nothing remaps, nothing searches by name.
package pose

import (
	"errors"
	"fmt"
	"math"

	"mgs2-restoration/cutscene-studio/internal/model"
)

// Skeleton is the model's rest hierarchy in glTF skin order, so an index here indexes
// three.js's skeleton.bones directly.
type Skeleton struct {
	Parents []int32      // -1 for a root
	Rest    [][3]float32 // offset from the PARENT (model.Joint.Local -- NOT model.Joint.Rest, which is from the root)
	Names   []string     // synthesised: neither .kms nor .evm stores joint names
}

// Joint is one joint's raw pose for one frame. Mirrors diet.Joint / scene.Joint.
type Joint struct {
	Quat  [4]float32 // xyzw, parent-relative, as quantised (not necessarily unit)
	Trans [3]float32 // added to the joint's rest offset, in the parent's frame
}

// Bone is a parent-relative pose a renderer can hand straight to a three.js bone:
// bone.quaternion = Quat, bone.position = Trans.
type Bone struct {
	Quat  [4]float32 // xyzw, normalised
	Trans [3]float32 // rest offset from the parent + the frame's joint translation
}

// SkeletonOf lifts the rest hierarchy out of a decoded .kms or .evm. Joint order is the file's
// own unit/skeleton order, which is also the order model.WriteGLTF emits the skin in.
func SkeletonOf(m *model.Mesh) (*Skeleton, error) {
	if m == nil {
		return nil, errors.New("pose: nil mesh")
	}
	n := len(m.Skeleton)
	if n == 0 {
		return nil, fmt.Errorf("pose: %s has no skeleton", m.Source)
	}
	sk := &Skeleton{
		Parents: make([]int32, n),
		Rest:    make([][3]float32, n),
		Names:   make([]string, n),
	}
	for i, j := range m.Skeleton {
		p := j.Parent
		if p < 0 || int(p) >= n || int(p) == i {
			p = -1
		}
		sk.Parents[i] = p
		sk.Rest[i] = j.Local
		sk.Names[i] = fmt.Sprintf("joint%d", i) // matches model/gltf.go's node names
	}
	if err := sk.check(); err != nil {
		return nil, fmt.Errorf("pose: %s: %w", m.Source, err)
	}
	return sk, nil
}

// check rejects a hierarchy we could not evaluate -- a cycle would hang the world-matrix walk.
func (sk *Skeleton) check() error {
	const (
		todo = iota
		busy
		done
	)
	state := make([]byte, len(sk.Parents))
	for i := range sk.Parents {
		for j := i; j >= 0 && state[j] != done; {
			if state[j] == busy {
				return fmt.Errorf("joint %d sits in a parent cycle", j)
			}
			state[j] = busy
			j = int(sk.Parents[j])
		}
		for j := i; j >= 0 && state[j] == busy; j = int(sk.Parents[j]) {
			state[j] = done
		}
	}
	return nil
}

func (sk *Skeleton) Len() int { return len(sk.Parents) }

// Roots lists the joints whose parent is the object centre. A character has exactly one; some
// rigid props have several, and a glTF skin only ever parents the FIRST into the scene.
func (sk *Skeleton) Roots() []int {
	var out []int
	for i, p := range sk.Parents {
		if p < 0 {
			out = append(out, i)
		}
	}
	return out
}

// Pose maps one frame of demo joint data onto the skeleton.
//
// len(joints) is the MOTION_PACK's n_joints and does NOT have to equal the skeleton size, in
// either direction. It matches on 765 of the 991 skinned objects in the shipped demos:
//   - Fewer (71): a .kms is n_models base units plus extension units up to n_x_models, and the
//     exporter only wrote motion for the base rig -- amg_def 3 of 5, ray_def_mt 40 of 44. The
//     extras hold their rest pose because InitMotion cleared the whole array to identity first
//     (obj_act.c:340; DG_ZeroVector is {0,0,0,1}, not zero).
//   - More (155): 151 objects declare exactly one extra joint, and it is a padded (0,0,0,-1);
//     4 are body-only EVM rigs (rai_gbs_body, 21 joints) fed the full 53-joint character stream.
//     LoadMotion writes the surplus into the scratchpad; the matrix loop, bounded by the
//     skeleton, never reads it. Surplus is DROPPED, never shifted in.
func Pose(sk *Skeleton, joints []Joint) ([]Bone, error) {
	if sk == nil {
		return nil, errors.New("pose: nil skeleton")
	}
	out := make([]Bone, len(sk.Parents))
	for i := range out {
		out[i] = Bone{Quat: [4]float32{0, 0, 0, 1}, Trans: sk.Rest[i]}
		if i >= len(joints) {
			continue
		}
		j := joints[i]
		out[i].Quat = normalize(j.Quat)
		for a := 0; a < 3; a++ {
			out[i].Trans[a] = sk.Rest[i][a] + j.Trans[a]
		}
	}
	return out, nil
}

// The stream stores quats as (int)(f*16383) and the game normalises straight after the temporal
// mask is resolved (demo_mtn.c:488 MT_QuatNormalize), so decoded norms land within ~2% of unit.
func normalize(q [4]float32) [4]float32 {
	d := math.Sqrt(float64(q[0]*q[0] + q[1]*q[1] + q[2]*q[2] + q[3]*q[3]))
	if d < 1e-6 {
		return [4]float32{0, 0, 0, 1}
	}
	s := float32(1 / d)
	return [4]float32{q[0] * s, q[1] * s, q[2] * s, q[3] * s}
}

// ObjectMatrix is the object centre: DG_SetPos2 (wpos.c:283). Angles are 12-bit fixed, sign-
// extended, value*pi/2048 -- so 4096 is a full turn, not a quarter. Rotations apply Z, then X,
// then Y, i.e. M = Ry*Rx*Rz.
//
// Column-major, three.js/glTF order: translation is at [12],[13],[14]. (The engine stores the
// transpose -- row vectors, translation in row 3 -- so do not copy its m[][] layout across.)
func ObjectMatrix(pos [3]float32, rot [3]int16) [16]float32 {
	rx := angle(rot[0])
	ry := angle(rot[1])
	rz := angle(rot[2])
	sx, cx := math.Sincos(rx)
	sy, cy := math.Sincos(ry)
	sz, cz := math.Sincos(rz)

	// R = Ry*Rx*Rz, expanded.
	m := [9]float64{
		cy*cz + sy*sx*sz, -cy*sz + sy*sx*cz, sy * cx, // column 0..2 of row 0
		cx * sz, cx * cz, -sx,
		-sy*cz + cy*sx*sz, sy*sz + cy*sx*cz, cy * cx,
	}
	var out [16]float32
	// m[] above is row-major 3x3; glTF wants column-major, so index [col*4+row] = m[row*3+col].
	for r := 0; r < 3; r++ {
		for c := 0; c < 3; c++ {
			out[c*4+r] = float32(m[r*3+c])
		}
	}
	out[12], out[13], out[14] = pos[0], pos[1], pos[2]
	out[15] = 1
	return out
}

// REGULAR_ANGLE (wpos.c:143): keep the low 12 bits, sign-extend. Retail ships vy = 2802, which
// the engine reads as -1294.
func angle(v int16) float64 {
	a := int32(int16(uint16(v)<<4) >> 4)
	return float64(a) * math.Pi / 2048
}

// Matrices walks the bones to world space: world[i] = world[parent] * translate(Trans) * rot(Quat),
// with the object matrix standing in for parent -1. That is the same product the engine builds in
// Scrpad.matrix[], and it is what an effect's link vector (object_id, joint_index) resolves to via
// DM_GetObjectMatrix. Column-major, like ObjectMatrix.
//
// Note this is the BONE matrix, not the skinning matrix: to skin, post-multiply by the inverse
// bind, which for both formats is a pure translate(-rest_from_root) (see model/gltf.go).
func Matrices(sk *Skeleton, bones []Bone, object [16]float32) ([][16]float32, error) {
	if sk == nil {
		return nil, errors.New("pose: nil skeleton")
	}
	if len(bones) != len(sk.Parents) {
		return nil, fmt.Errorf("pose: %d bones for a %d-joint skeleton", len(bones), len(sk.Parents))
	}
	out := make([][16]float32, len(bones))
	done := make([]bool, len(bones))
	var resolve func(i int) [16]float32
	resolve = func(i int) [16]float32 {
		if done[i] {
			return out[i]
		}
		done[i] = true // SkeletonOf already rejected cycles; this just stops a re-entry loop
		parent := object
		if p := sk.Parents[i]; p >= 0 {
			parent = resolve(int(p))
		}
		out[i] = mul(parent, local(bones[i]))
		return out[i]
	}
	for i := range out {
		resolve(i)
	}
	return out, nil
}

// local is translate(Trans) * rot(Quat) -- the rotation happens in the joint's own frame.
func local(b Bone) [16]float32 {
	x, y, z, w := float64(b.Quat[0]), float64(b.Quat[1]), float64(b.Quat[2]), float64(b.Quat[3])
	x2, y2, z2 := x+x, y+y, z+z
	xx, xy, xz := x*x2, x*y2, x*z2
	yy, yz, zz := y*y2, y*z2, z*z2
	wx, wy, wz := w*x2, w*y2, w*z2
	return [16]float32{
		float32(1 - (yy + zz)), float32(xy + wz), float32(xz - wy), 0,
		float32(xy - wz), float32(1 - (xx + zz)), float32(yz + wx), 0,
		float32(xz + wy), float32(yz - wx), float32(1 - (xx + yy)), 0,
		b.Trans[0], b.Trans[1], b.Trans[2], 1,
	}
}

// mul is a*b, column-major, applying b first.
func mul(a, b [16]float32) [16]float32 {
	var r [16]float32
	for c := 0; c < 4; c++ {
		for row := 0; row < 4; row++ {
			var s float32
			for k := 0; k < 4; k++ {
				s += a[k*4+row] * b[c*4+k]
			}
			r[c*4+row] = s
		}
	}
	return r
}

// Apply transforms a point by a column-major matrix (w = 1).
func Apply(m [16]float32, v [3]float32) [3]float32 {
	return [3]float32{
		m[0]*v[0] + m[4]*v[1] + m[8]*v[2] + m[12],
		m[1]*v[0] + m[5]*v[1] + m[9]*v[2] + m[13],
		m[2]*v[0] + m[6]*v[1] + m[10]*v[2] + m[14],
	}
}
