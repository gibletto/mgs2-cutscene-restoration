package pose

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"mgs2-restoration/cutscene-studio/internal/model"
	"mgs2-restoration/cutscene-studio/internal/scene"
	"mgs2-restoration/cutscene-studio/internal/testenv"
)

// The corpus tests: everything here is a claim about the shipped data, checked against it.

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func gameRoot(t *testing.T) string {
	t.Helper()
	dir := testenv.GameDir()
	if _, err := os.Stat(filepath.Join(dir, "us", "demo2", "_bp")); err != nil {
		t.Skipf("no game install at %s (set MGS2_DIR)", dir)
	}
	return dir
}

func gameIndex(t *testing.T) *model.Index {
	t.Helper()
	dir := testenv.GameDataDir()
	ix, err := model.LoadIndex(dir)
	if err != nil {
		t.Skipf("no GameData at %s (set MGS2_GAMEDATA): %v", dir, err)
	}
	ix.SetAssetRoot(dir)
	return ix
}

var meshes = map[string]*model.Mesh{}

// meshOf resolves a DEMO_OBJECT cache id to a decoded model. Cache id alone is ambiguous by
// design; the variants differ in geometry, not in joint count, which is all
// these tests read, so the first on-disk hit is enough here and nowhere else.
func meshOf(ix *model.Index, cacheID int32) (*model.Mesh, string) {
	if cacheID == 0 {
		return nil, ""
	}
	id, spec := model.SplitCacheID(cacheID)
	ext := "kms"
	if spec == 'e' {
		ext = "evm"
	}
	for _, a := range ix.Resolve(id, ext) {
		if !ix.Exists(a) {
			continue
		}
		p := ix.FilePath(a)
		m, seen := meshes[p]
		if !seen {
			m, _ = model.LoadModel(p)
			meshes[p] = m
		}
		if m != nil {
			return m, a.Name
		}
	}
	return nil, ""
}

func demos(t *testing.T) []string {
	t.Helper()
	root := gameRoot(t)
	var out []string
	for _, d := range []string{filepath.Join(root, "us", "demo2", "_bp"), filepath.Join(root, "us", "demo", "_bp")} {
		ents, _ := os.ReadDir(d)
		for _, e := range ents {
			if filepath.Ext(e.Name()) == ".sdt" {
				out = append(out, filepath.Join(d, e.Name()))
			}
		}
	}
	sort.Strings(out)
	return out
}

func load(t *testing.T, path string, opt scene.Opts) *scene.Scene {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := scene.ImportWith(path, raw, nil, opt)
	if err != nil {
		t.Fatalf("%s: %v", filepath.Base(path), err)
	}
	return sc
}

// Skinned is the rule for whether an object's joints reach the renderer at all.
//
// GetResources (obj_act.c:982-1053) picks set_motion in two steps: the KMS branch installs
// SetMotionKms and sets onepiece_flag when (flag & 3) == 0; the EVM branch then OVERWRITES
// set_motion with SetMotionEvm if there is an evm cache_id. SetMotionKms returns early when
// onepiece_flag bit 0 is set (obj_act.c:400). SetMotionEvm has no such guard. Hence:
func skinnedObject(o *scene.ObjectTrack) bool { return o.CacheID != 0 || o.Flag&3 != 0 }

// TestJointsIndexTheSkeleton is the mapping rule: demo joint i drives skeleton joint i, and the
// two counts do not have to agree in either direction.
//
// n_joints is NOT the model's unit count. For a KMS it is n_models, the BASE rig, while the
// skeleton the engine allocates and walks is n_x_models -- the base rig plus extension units
// (objs->n_models = def->n_x_models, objs.c:146). amg_def is 3 of 5, ray_def_mt 40 of 44,
// cit_femalec_def 21 of 23. The extension units hold their rest pose, because InitMotion cleared
// the whole array to DG_ZeroVector = {0,0,0,1} before LoadMotion wrote anything (obj_act.c:340).
//
// It runs the other way too. Body-only EVMs (rai_gbs_body: 21 joints, sol_vanim_0_sh: 23) get the
// full 53-joint character stream; the matrix loop is bounded by the skeleton, so joints past the
// end are written into the scratchpad and never read. Surplus is DROPPED, never shifted in.
func TestJointsIndexTheSkeleton(t *testing.T) {
	ix := gameIndex(t)
	hist := map[string]int{}
	for _, p := range demos(t) {
		for _, o := range load(t, p, scene.Opts{}).Objects {
			if !skinnedObject(o) || o.NJoints == 0 {
				continue
			}
			cid := o.CacheID
			if cid == 0 {
				cid = o.KMSCacheID
			}
			m, name := meshOf(ix, cid)
			if m == nil {
				hist["unresolved"]++
				continue
			}
			sk, err := SkeletonOf(m)
			if err != nil {
				t.Errorf("%s obj %d (%s): %v", filepath.Base(p), o.ID, name, err)
				continue
			}
			// Whatever the counts, a frame poses cleanly and returns one bone per skeleton joint.
			bones, err := Pose(sk, make([]Joint, o.NJoints))
			if err != nil || len(bones) != m.Joints {
				t.Errorf("%s obj %d (%s): Pose(%d joints onto %d) = %d bones, %v",
					filepath.Base(p), o.ID, name, o.NJoints, m.Joints, len(bones), err)
			}
			switch {
			case o.NJoints == m.Joints:
				hist["exact"]++
			case o.NJoints < m.Joints:
				hist["prefix (extension units hold rest)"]++
			case o.NJoints == m.Joints+1:
				hist["one surplus joint"]++
			default:
				hist["surplus (body-only rig)"]++
			}
		}
	}
	for k, v := range hist {
		t.Logf("%-38s %d", k, v)
	}
	if hist["exact"] == 0 {
		t.Fatal("resolved nothing -- the index is not pointing at the models")
	}
}

// TestShadowRigIsAPrefixOfTheEVMRig is the structural proof of the identity mapping.
//
// An EVM object also carries a KMS "shadow" body, and SetKmsMatrixFromEvm (obj_act.c:520-556)
// copies EVM joint matrix i straight onto KMS unit i -- no name lookup, no remap, just two
// pointers walking in step. So the 21-unit shadow rig must BE the first 21 joints of the 53-joint
// character rig. If it is, joint index is a shared, stable identity across rigs, and a demo's
// joint i can only mean skeleton joint i.
func TestShadowRigIsAPrefixOfTheEVMRig(t *testing.T) {
	ix := gameIndex(t)
	root := gameRoot(t)
	checked := 0
	for _, o := range load(t, filepath.Join(root, "us", "demo2", "_bp", "p080_05_p03.sdt"), scene.Opts{}).Objects {
		if o.CacheID == 0 {
			continue
		}
		km, kn := meshOf(ix, o.KMSCacheID)
		em, en := meshOf(ix, o.CacheID)
		if km == nil || em == nil {
			continue
		}
		n := min(km.Joints, em.Joints)
		for i := 0; i < n; i++ {
			if km.Skeleton[i].Parent != em.Skeleton[i].Parent {
				t.Errorf("obj %d: %s unit %d has parent %d but %s joint %d has parent %d -- "+
					"the rigs do not share joint indices", o.ID, kn, i, km.Skeleton[i].Parent,
					en, i, em.Skeleton[i].Parent)
			}
		}
		t.Logf("obj %-3d %-22s (%2d units) is the first %d joints of %-38s (%d joints)",
			o.ID, kn, km.Joints, n, en, em.Joints)
		checked++
	}
	if checked < 3 {
		t.Fatalf("only checked %d EVM objects", checked)
	}
}

// TestOnepieceJointsAreEmpty. flag&3 == 0 is DG_FLAG_ONEPIECE and SetMotionKms discards the joint
// data. Belt and braces: Konami's exporter never put anything there to discard. Every one of the
// ~1,050 KMS-only rigid objects in the shipped demos ships all-identity joints -- the no-op
// 0x7F00|run block. A rigid prop is placed by pos+rot and nothing else.
func TestOnepieceJointsAreEmpty(t *testing.T) {
	_ = gameRoot(t)
	var rigid, moving int
	for _, p := range demos(t) {
		for _, o := range load(t, p, scene.Opts{Joints: true}).Objects {
			if skinnedObject(o) {
				continue
			}
			rigid++
			for _, fr := range o.Joints {
				for _, j := range fr {
					q := normalize(j.Quat)
					ang := 2 * math.Acos(math.Min(1, math.Abs(float64(q[3]))))
					tr := math.Sqrt(float64(j.Trans[0]*j.Trans[0] + j.Trans[1]*j.Trans[1] + j.Trans[2]*j.Trans[2]))
					if ang > 0.02 || tr > 0.5 {
						moving++
						t.Errorf("%s obj %d: a ONEPIECE object carries joint motion (%.1f deg, %.1f units) -- "+
							"the engine drops it, so check before posing it", filepath.Base(p), o.ID, ang*180/math.Pi, tr)
					}
				}
			}
		}
	}
	t.Logf("%d rigid (flag&3==0, KMS-only) objects, %d with any joint motion at all", rigid, moving)
}

// TestPosedFeetLandOnTheFloor is the physical check on the whole chain.
//
// A demo hard-sets an object's centre every frame and the engine floor-checks it (Act():
// HZX_LevelHazardCheck on ctrl->mov). A character rig's origin is its floor contact, roughly
// 1,100 units below the hip the mesh is modelled around. So for a character standing on the
// ground, the posed, world-space mesh's lowest vertex must sit at pos.y.
//
// That is one number that only comes out right if the quaternion handedness, the parent order,
// the object's 12-bit Ry.Rx.Rz triple AND the joint->unit mapping are all right. It lands within
// single-digit units (millimetres) on the standing cast of the Arsenal demos. Conjugating the
// quaternions costs 30-500 units; shifting the joint mapping by one costs ~1,000 -- the whole
// character floats a metre off the floor.
func TestPosedFeetLandOnTheFloor(t *testing.T) {
	ix := gameIndex(t)
	root := gameRoot(t)
	// The Arsenal roof: Snake, Raiden, Solidus, Fortune, Ocelot, all on their feet on a flat deck.
	sc := load(t, filepath.Join(root, "us", "demo2", "_bp", "p080_11_p06.sdt"), scene.Opts{Joints: true})

	checked := 0
	for _, o := range sc.Objects {
		if o.CacheID == 0 || len(o.Joints) == 0 {
			continue
		}
		m, name := meshOf(ix, o.CacheID)
		if m == nil || m.Joints < 40 || m.Min[1] > -900 { // full character rigs only, not face attachments
			continue
		}
		sk, err := SkeletonOf(m)
		if err != nil {
			t.Fatal(err)
		}
		var dy []float64
		for f := 0; f < len(o.Joints); f += len(o.Joints)/100 + 1 {
			if len(o.Joints[f]) == 0 {
				continue
			}
			pos := [3]float32{float32(o.PosX.Sample(f)), float32(o.PosY.Sample(f)), float32(o.PosZ.Sample(f))}
			rot := [3]int16{int16(o.RotX.Sample(f)), int16(o.RotY.Sample(f)), int16(o.RotZ.Sample(f))}
			bones, err := Pose(sk, jointsOf(o.Joints[f]))
			if err != nil {
				t.Fatal(err)
			}
			mats, err := Matrices(sk, bones, ObjectMatrix(pos, rot))
			if err != nil {
				t.Fatal(err)
			}
			lo, _ := skinBounds(m, mats)
			dy = append(dy, math.Abs(float64(lo[1]-pos[1])))
		}
		if len(dy) < 10 {
			continue
		}
		sort.Float64s(dy)
		med := dy[len(dy)/2]
		t.Logf("obj %-3d %-36s median |feet - floor| = %.0f units", o.ID, name, med)
		if med > 120 {
			t.Errorf("obj %d (%s): posed feet sit %.0f units off the floor -- the pose is wrong", o.ID, name, med)
		}
		checked++
	}
	if checked < 3 {
		t.Fatalf("only checked %d characters", checked)
	}
}

func jointsOf(js []scene.Joint) []Joint {
	out := make([]Joint, len(js))
	for i, j := range js {
		out[i] = Joint{Quat: j.Quat, Trans: j.Trans}
	}
	return out
}

// skinBounds applies the skin the way the viewport does: bone matrix times the inverse bind,
// which for both formats is a pure translate(-rest_from_root) (model/gltf.go:278).
func skinBounds(m *model.Mesh, mats [][16]float32) (lo, hi [3]float32) {
	lo = [3]float32{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}
	hi = [3]float32{-math.MaxFloat32, -math.MaxFloat32, -math.MaxFloat32}
	for v := 0; v < m.Verts(); v++ {
		p := [3]float32{m.Positions[3*v], m.Positions[3*v+1], m.Positions[3*v+2]}
		var acc [3]float32
		var sum float32
		for k := 0; k < 4; k++ {
			w := m.Weights[4*v+k]
			b := int(m.Bones[4*v+k])
			if w <= 0 || b >= len(mats) {
				continue
			}
			rest := m.Skeleton[b].Rest
			q := Apply(mats[b], [3]float32{p[0] - rest[0], p[1] - rest[1], p[2] - rest[2]})
			for a := 0; a < 3; a++ {
				acc[a] += w * q[a]
			}
			sum += w
		}
		for a := 0; a < 3; a++ {
			if sum > 0 {
				acc[a] /= sum
			}
			if acc[a] < lo[a] {
				lo[a] = acc[a]
			}
			if acc[a] > hi[a] {
				hi[a] = acc[a]
			}
		}
	}
	return lo, hi
}
