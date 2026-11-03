package pose

import (
	"math"
	"testing"

	"mgs2-restoration/cutscene-studio/internal/model"
)

func near(a, b, tol float32) bool { return math.Abs(float64(a-b)) <= float64(tol) }

func closeVec(a, b [3]float32, tol float32) bool {
	return near(a[0], b[0], tol) && near(a[1], b[1], tol) && near(a[2], b[2], tol)
}

// A tiny two-limb rig: root, one child up, one grandchild out to the side.
func testMesh() *model.Mesh {
	return &model.Mesh{
		Kind:    "kms",
		Source:  "test",
		Skinned: true,
		Joints:  4,
		Skeleton: []model.Joint{
			{Parent: -1, Local: [3]float32{0, 0, 0}, Rest: [3]float32{0, 0, 0}},
			{Parent: 0, Local: [3]float32{0, 900, 0}, Rest: [3]float32{0, 900, 0}},
			{Parent: 1, Local: [3]float32{200, 0, 0}, Rest: [3]float32{200, 900, 0}},
			{Parent: 2, Local: [3]float32{300, 0, 0}, Rest: [3]float32{500, 900, 0}}, // an unanimated tip
		},
	}
}

func TestSkeletonOf(t *testing.T) {
	sk, err := SkeletonOf(testMesh())
	if err != nil {
		t.Fatal(err)
	}
	if sk.Len() != 4 {
		t.Fatalf("Len() = %d, want 4", sk.Len())
	}
	if got := sk.Roots(); len(got) != 1 || got[0] != 0 {
		t.Errorf("Roots() = %v, want [0]", got)
	}
	if sk.Rest[2] != ([3]float32{200, 0, 0}) {
		t.Errorf("Rest[2] = %v, want the offset from the PARENT (200,0,0), not from the root", sk.Rest[2])
	}

	if _, err := SkeletonOf(&model.Mesh{}); err == nil {
		t.Error("a mesh with no skeleton should be an error")
	}
	cyc := testMesh()
	cyc.Skeleton[0].Parent = 3
	if _, err := SkeletonOf(cyc); err == nil {
		t.Error("a parent cycle should be an error")
	}
	// A parent that indexes nothing is a root, like the engine's parent == -1.
	orphan := testMesh()
	orphan.Skeleton[1].Parent = 99
	sk, err = SkeletonOf(orphan)
	if err != nil {
		t.Fatal(err)
	}
	if sk.Parents[1] != -1 {
		t.Errorf("out-of-range parent should normalise to -1, got %d", sk.Parents[1])
	}
}

func TestPoseHoldsRestPose(t *testing.T) {
	sk, _ := SkeletonOf(testMesh())

	// No motion at all: every bone sits at its rest offset, rotation identity.
	bones, err := Pose(sk, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, b := range bones {
		if b.Quat != ([4]float32{0, 0, 0, 1}) {
			t.Errorf("bone %d: quat %v, want identity", i, b.Quat)
		}
		if b.Trans != sk.Rest[i] {
			t.Errorf("bone %d: trans %v, want rest %v", i, b.Trans, sk.Rest[i])
		}
	}

	// Fewer joints than the skeleton -- amg_def's 3-of-5, ray_def_mt's 40-of-44. The tail holds.
	q := [4]float32{0, 0.7071, 0, 0.7071}
	bones, _ = Pose(sk, []Joint{{Quat: q}, {Quat: q}})
	if bones[3].Quat != ([4]float32{0, 0, 0, 1}) || bones[3].Trans != sk.Rest[3] {
		t.Errorf("unanimated tail joint should hold its rest pose, got %+v", bones[3])
	}

	// More joints than the skeleton -- 152 shipped objects do this, always by exactly one.
	// The surplus is dropped, NOT shifted in.
	surplus := make([]Joint, 5)
	for i := range surplus {
		surplus[i] = Joint{Quat: [4]float32{0, 0, 0, 1}}
	}
	surplus[0].Trans = [3]float32{1, 2, 3}
	bones, err = Pose(sk, surplus)
	if err != nil {
		t.Fatalf("a surplus joint must not be an error: %v", err)
	}
	if len(bones) != 4 {
		t.Fatalf("got %d bones for a 4-joint skeleton", len(bones))
	}
	if want := ([3]float32{1, 2, 3}); bones[0].Trans != want {
		t.Errorf("joint 0 drives bone 0: trans %v, want %v", bones[0].Trans, want)
	}
}

// The stream quantises to (int)(f*16383) and the game normalises after the temporal mask is
// resolved. A zero quat -- which retail does ship -- must fall back to identity, exactly as
// MT_QuatNormalize's `square > 0.00001` guard plus MT_QuatToMat do (quat2.c:250,420).
func TestPoseNormalisesQuats(t *testing.T) {
	sk, _ := SkeletonOf(testMesh())
	bones, _ := Pose(sk, []Joint{{Quat: [4]float32{0, 0, 0, 0}}, {Quat: [4]float32{0, 2, 0, 0}}})
	if bones[0].Quat != ([4]float32{0, 0, 0, 1}) {
		t.Errorf("zero quat should read as identity, got %v", bones[0].Quat)
	}
	if !near(bones[1].Quat[1], 1, 1e-5) {
		t.Errorf("quat should come back unit, got %v", bones[1].Quat)
	}
}

func TestObjectMatrix(t *testing.T) {
	// 12-bit fixed: 4096 is a FULL turn, so it wraps to identity.
	if m := ObjectMatrix([3]float32{}, [3]int16{0, 4096, 0}); !near(m[0], 1, 1e-5) || !near(m[10], 1, 1e-5) {
		t.Errorf("4096 should be 360 deg (identity), got %v", m)
	}
	// 1024 == +90 deg about Y: +X goes to -Z (right-handed).
	m := ObjectMatrix([3]float32{}, [3]int16{0, 1024, 0})
	if got := Apply(m, [3]float32{100, 0, 0}); !closeVec(got, [3]float32{0, 0, -100}, 0.01) {
		t.Errorf("Ry(90) * (100,0,0) = %v, want (0,0,-100)", got)
	}
	// Values are masked to 12 bits and SIGN-EXTENDED: retail's vy=2802 reads as -1294.
	a, b := ObjectMatrix([3]float32{}, [3]int16{0, 2802, 0}), ObjectMatrix([3]float32{}, [3]int16{0, -1294, 0})
	for i := range a {
		if !near(a[i], b[i], 1e-5) {
			t.Fatalf("REGULAR_ANGLE: 2802 must read as -1294\n%v\n%v", a, b)
		}
	}
	// Translation lands in the glTF/three.js slots, not the engine's row 3.
	m = ObjectMatrix([3]float32{10, 20, 30}, [3]int16{})
	if m[12] != 10 || m[13] != 20 || m[14] != 30 || m[15] != 1 {
		t.Errorf("translation should be column-major at [12..14], got %v", m)
	}
	// Order is Ry*Rx*Rz -- Z first. Swap two and the answer moves, so the test bites.
	m = ObjectMatrix([3]float32{}, [3]int16{1024, 1024, 0})
	if got := Apply(m, [3]float32{0, 100, 0}); !closeVec(got, [3]float32{100, 0, 0}, 0.01) {
		t.Errorf("Ry(90)*Rx(90) * (0,100,0) = %v, want (100,0,0)", got)
	}
}

// The whole point: Matrices() must reproduce the engine's own joint matrices.
//
// engineEVM below follows the arithmetic of MakeMatrixFromSkeleton (obj_act.c:470-518) step
// by step -- row-vector matrices, EVM_SKEL.rt as the offset from the ROOT, and the -rt
// correction back to the hip position that folds the inverse bind into the joint matrix. If our claim is right, then
//
//	engine[i] == Matrices[i] * translate(-rest_from_root[i])
//
// for every joint, which is exactly the statement that our bone matrices are the joint matrices
// and the glTF's translate(-Rest) inverse bind is the missing factor.
func TestMatricesMatchEngine(t *testing.T) {
	m := testMesh()
	sk, _ := SkeletonOf(m)
	joints := []Joint{
		{Quat: [4]float32{0.2, 0.3, 0.1, 0.927}, Trans: [3]float32{3, -4, 5}},
		{Quat: [4]float32{-0.5, 0.5, 0.5, 0.5}, Trans: [3]float32{0, 12, 0}},
		{Quat: [4]float32{0, 0.3827, 0, 0.9239}, Trans: [3]float32{-2, 0, 7}},
	}
	bones, _ := Pose(sk, joints)
	object := ObjectMatrix([3]float32{1234, 56, -789}, [3]int16{100, 2802, -300})
	ours, err := Matrices(sk, bones, object)
	if err != nil {
		t.Fatal(err)
	}

	eng := engineEVM(m, joints, object)
	for i := range eng {
		want := mul(ours[i], translate(neg(m.Skeleton[i].Rest)))
		for k := range want {
			if !near(eng[i][k], want[k], 0.02) {
				t.Fatalf("joint %d does not match the engine\nengine %v\nours*ibm %v", i, eng[i], want)
			}
		}
	}
}

// engineEVM re-expresses obj_act.c:470-518 with this package's helpers. Everything is converted to column-major here so
// it can be compared, but the arithmetic is the engine's: rot from the PARENT'S FINAL matrix,
// translation through the parent's final matrix applied to (rt + trans), then the -rt fixup.
func engineEVM(m *model.Mesh, joints []Joint, world [16]float32) [][16]float32 {
	mats := make([][16]float32, len(m.Skeleton))
	for i, s := range m.Skeleton {
		var rot [4]float32
		var trans [3]float32
		if i < len(joints) {
			rot, trans = normalize(joints[i].Quat), joints[i].Trans
		} else {
			rot = [4]float32{0, 0, 0, 1} // InitMotion: DG_ZeroVector is {0,0,0,1}
		}
		parent := world
		if p := int(s.Parent); p >= 0 {
			parent = mats[p]
		}
		mat := mul(parent, local(Bone{Quat: rot})) // rotation only; translation comes next
		p := Apply(parent, [3]float32{s.Rest[0] + trans[0], s.Rest[1] + trans[1], s.Rest[2] + trans[2]})
		mat[12], mat[13], mat[14] = p[0], p[1], p[2]
		back := Apply(mat, neg(s.Rest))
		mat[12], mat[13], mat[14] = back[0], back[1], back[2]
		mats[i] = mat
	}
	return mats
}

func neg(v [3]float32) [3]float32 { return [3]float32{-v[0], -v[1], -v[2]} }

func translate(v [3]float32) [16]float32 {
	return [16]float32{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, v[0], v[1], v[2], 1}
}

// The KMS poser (obj_act.c:376-468) has no -rt correction because its vertices are bone-local.
// Our bone matrices ARE its matrices, with no inverse bind at all.
func TestMatricesMatchEngineKMS(t *testing.T) {
	m := testMesh()
	sk, _ := SkeletonOf(m)
	joints := []Joint{
		{Quat: [4]float32{0.1, -0.2, 0.3, 0.927}, Trans: [3]float32{1, 2, 3}},
		{Quat: [4]float32{0, 0.7071, 0, 0.7071}},
	}
	bones, _ := Pose(sk, joints)
	object := ObjectMatrix([3]float32{0, 0, 0}, [3]int16{0, 512, 0})
	ours, _ := Matrices(sk, bones, object)

	mats := make([][16]float32, len(m.Skeleton))
	for i, s := range m.Skeleton {
		rot := [4]float32{0, 0, 0, 1}
		var trans [3]float32
		if i < len(joints) {
			rot, trans = normalize(joints[i].Quat), joints[i].Trans
		}
		parent := object
		if p := int(s.Parent); p >= 0 {
			parent = mats[p]
		}
		mat := mul(parent, local(Bone{Quat: rot}))
		// obj->trans is DG_MDL.tx/ty/tz -- the LOCAL offset, so Local not Rest.
		p := Apply(parent, [3]float32{s.Local[0] + trans[0], s.Local[1] + trans[1], s.Local[2] + trans[2]})
		mat[12], mat[13], mat[14] = p[0], p[1], p[2]
		mats[i] = mat
	}
	for i := range mats {
		for k := range mats[i] {
			if !near(ours[i][k], mats[i][k], 0.02) {
				t.Fatalf("joint %d\nengine %v\nours   %v", i, mats[i], ours[i])
			}
		}
	}
}

// A jointless model is still placeable: the rest pose plus the object matrix is the whole answer
// for the 1,065 flag=0 objects in the shipped demos.
func TestMatricesRestPoseIsIdentitySkin(t *testing.T) {
	m := testMesh()
	sk, _ := SkeletonOf(m)
	bones, _ := Pose(sk, nil)
	mats, _ := Matrices(sk, bones, [16]float32{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1})
	for i, s := range m.Skeleton {
		// bone * inverse-bind == identity in the rest pose, so rest-pose vertices pass through.
		got := mul(mats[i], translate(neg(s.Rest)))
		want := [16]float32{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
		for k := range want {
			if !near(got[k], want[k], 1e-4) {
				t.Fatalf("joint %d: rest-pose skin matrix is not identity: %v", i, got)
			}
		}
	}
}

func TestMatricesRejectsMismatch(t *testing.T) {
	sk, _ := SkeletonOf(testMesh())
	if _, err := Matrices(sk, make([]Bone, 2), [16]float32{}); err == nil {
		t.Error("a bone count that does not match the skeleton should be an error")
	}
}
