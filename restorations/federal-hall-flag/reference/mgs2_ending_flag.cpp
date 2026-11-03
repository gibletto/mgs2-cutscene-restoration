#include "stdafx.h"
#include "mgs2_ending_flag.hpp"

#include "common.hpp"
#include "gamevars.hpp"
#include "game_stages.hpp"
#include "helper.hpp"
#include "logging.hpp"

namespace
{
    // Script names, hashed the way the compiler hashed them (EUC-JP bytes).
    constexpr int kFlagModel = 0x9d798f;        // w61a_flag, the waving flag on the pole
    constexpr int kFlagName = 0x8c086c;         // アメリカの旗, the name the script messages
    constexpr int kFallModel = 0xf0a25c;        // w61a_flag_fall1, the streamed fall animation
    constexpr int kFallName = 0x6a020;          // 落下星条旗
    constexpr int kPoleName = 0x6a030;          // the street flags answer to nothing, they just need names
    constexpr int kProcFlagOn = 0xf53f0a;       // 旗オン
    constexpr int kProcFlagOff = 0xf53eec;      // 旗オフ
    constexpr int kProcFallenOn = 0x391ca1;     // 静止星条旗オン
    constexpr int kProcFallenOff = 0x391c83;    // 静止星条旗オフ
    constexpr int kProcVampOn = 0xbfa10;        // ヴァンプオン, only the third demo sends it at frame 2

    // The script's own numbers for the flag: -b 5 -w 1 -p 0,0,0. The fallen flag sits at 0,2600,23600.
    constexpr int kBaseFrames = 5;
    constexpr int kRandomBits = 1;
    constexpr float kFallPos[4] = { 0.0f, 2600.0f, 23600.0f, 1.0f };

    // p082_01_p01: the shot cuts to Solidus at 5066 with the flag three metres above him. The fall
    // animation meets the ground at its frame 162, so play from 137 slowly enough to land at 5310.
    constexpr int kSetupFrame = 3;
    constexpr int kChopFrame = 5066;
    constexpr int kLandFrame = 5310;
    constexpr int kFallSkip = 137;
    constexpr int kTouchFrame = 162;
    constexpr int kFullRate = 4096;             // one animation frame per tick
    constexpr int kFallRate = kFullRate * (kTouchFrame - kFallSkip) / (kLandFrame - kChopFrame);

    // p082_04_p03: the officer (object 3) backs away with the flag from here until he stands still.
    // The edge nearest him moves first; each vertex waits until he has taken up the slack ahead of it.
    constexpr int kGrabFrame = 2747;
    constexpr int kOfficer = 3;
    constexpr int kRootJoint = 0;
    constexpr float kStillMm = 2.0f;
    constexpr int kStillFrames = 60;
    constexpr float kSlackPerMm = 0.6f;         // how far the bundle stretches before it all moves
    constexpr float kFlattenMm = 1500.0f;       // travel after the slack is gone that lays a vertex flat
    constexpr float kMinDragMm = 30.0f;
    constexpr float kDeflateStartMm = 1800.0f;  // by the end of the drag the tail has lost its shape
    constexpr float kDeflateMm = 1200.0f;

    // The steps under the drag, read from the map: the landing he stands on, then down to the ground.
    constexpr float kLandingY = 3000.0f;
    constexpr float kLandingZ = 22600.0f;
    constexpr float kGroundY = 2000.0f;
    constexpr float kGroundZ = 24100.0f;
    constexpr int kMaxSettleTicks = 300;

    // These demos ran at 30 on the PS2, so the flag took one wave step per two demo frames.
    // In the frame-skip slow-motion shots the screen only changed every other step.
    constexpr int kStepFrames = 2;
    constexpr int kSlowFactor = 2;
    struct Window { int from, to; };
    constexpr Window kSlowWindows[] = { { 0, 302 }, { 4806, 5074 } };

    // The flags on the street poles, from the map: where each pole meets the wall and the way it points.
    // Our flag model flies from a vertical pole, so it is turned to hang and stretched to a draped shape.
    struct PoleFlag { float x, y, z, yaw; };
    constexpr PoleFlag kPoleFlags[] = {
        {  15975.0f,  9130.0f, 29663.0f,  91.0f },
        {  37348.0f,  9060.0f, 29692.0f,  93.0f },
        {  44825.0f, 11620.0f, 42283.0f, -89.0f },
        {  53766.0f, 11620.0f, 42290.0f, -93.0f },
        {  62624.0f, 11630.0f, 42286.0f, -89.0f },
        {  91390.0f,  8060.0f, 28595.0f,  88.0f },
        {  95809.0f, 14520.0f, 42284.0f, -88.0f },
        {  99365.0f,  8060.0f, 28581.0f,  88.0f },
        { 107487.0f,  8060.0f, 28593.0f,  88.0f },
        { 109920.0f, 14520.0f, 42283.0f, -88.0f },
        { 124277.0f, 14510.0f, 42281.0f, -93.0f },
    };
    constexpr int kPoleCount = static_cast<int>(std::size(kPoleFlags));
    // The poles climb 23 degrees away from the wall. The cloth's own curl makes its top edge lag,
    // so it is laid at a steeper angle and lifted a little to follow the pole up to the ball.
    constexpr float kPoleRiseDeg = 31.0f;
    constexpr float kPoleLiftMm = 60.0f;
    constexpr float kPoleSideMm = 100.0f;       // the cloth hangs beside the pole, not through it
    constexpr float kPoleSpanMm = 1460.0f;      // how far out the pole holds the cloth
    constexpr float kPoleHangMm = 3460.0f;      // how far the cloth drapes
    constexpr float kModelClothMm = 2110.0f;    // our flag, measured: cloth out, top edge down the pole
    constexpr float kModelHoistMm = 1634.0f;
    constexpr float kPlumbShear = 0.474f;       // our flag droops as it flies; lean it back out to hang plumb
    // The corner of the cloth that meets the wall. The star field is at the far end of the top edge,
    // which puts it at the raised end of the pole, where a flag on an angled pole carries it.
    constexpr float kModelPivot[3] = { 584.0f, 24003.0f, 22500.0f };
    constexpr float kSwayDeg = 4.5f;
    constexpr int kSwayTicks = 210;             // the quickest swing
    constexpr int kSwaySpread = 130;            // how much slower the slowest one is
    constexpr float kSwaySecond = 0.61f;        // a second, shorter swing rides on the first
    constexpr int kHeldFrames = 3000;           // key spacing this long reads as one still pose
    constexpr float kPi = 3.14159265f;

    constexpr size_t kVanimeName = 0xF8;        // the waving flag's message name, unset by the Called ctor
    constexpr size_t kVanimeObjs = 0xE0;        // DG_OBJS*, its world matrix first
    constexpr size_t kVanimeMode = 0x104;       // negative = hold this tick
    constexpr size_t kStanimeRate = 0x150;      // 4096 = one animation frame per tick
    constexpr size_t kStanimeVerts = 0x154;
    constexpr size_t kStanimeMode = 0x160;      // bit 0 = still playing
    constexpr size_t kStanimeBuffers = 0x1C8;   // two short[3] arrays per frame: vertices, then normals
    constexpr int kStanimePlaying = 1;
    constexpr size_t kActorSignal = 0x50;       // every actor's signal function
    constexpr int kSignalGetMatrix = 4;         // DM_SIGNAL_OBJECT_GET_MATRIX
    constexpr size_t kMatrixTrans = 12;         // floats into an FMATRIX

    // An actor we made or found. Its Act stopping means the stage freed it.
    struct Actor
    {
        void* work = nullptr;
        bool seen = false;
        int missed = 0;

        void Check()
        {
            if (!work)
            {
                return;
            }
            missed = seen ? 0 : missed + 1;
            seen = false;
            if (missed > 1)
            {
                work = nullptr;
            }
        }
    };

    enum class Demo { Unknown, First, Second, Third };
    Demo gDemo = Demo::Unknown;
    int gLastFrame = -1;
    int gStepFrame = -1;
    Actor gFlag;
    Actor gFall;
    Actor gPoles[kPoleCount];
    bool gDragging = false;
    bool gDragDone = false;
    int gStillFrames = 0;
    float gDragStart[3] = {};
    float gLastRoot[3] = {};
    std::vector<int16_t> gRest;                 // the crumpled flag's vertices before the drag

    using NewPutVanimeObjectCalledFn = void* (*)(int id, int where, float* scale, int16_t* rot, float* pos, int base, int rand, int mode);
    using NewPutSTanimeObjectCallFn = void* (*)(int name, int model, void* obj, const float* pos, int16_t* rot, float* scale,
        const float* boundU, const float* boundL, int mode);
    using GetObjectHandleFn = void* (*)(int id);
    using SignalFn = int64_t (*)(void* work, int signal, int64_t value);
    NewPutVanimeObjectCalledFn NewPutVanimeObjectCalled = nullptr;
    NewPutSTanimeObjectCallFn NewPutSTanimeObjectCall = nullptr;
    GetObjectHandleFn DM_GetObjectHandle = nullptr;
    SafetyHookInline h_ExecProc {};
    SafetyHookInline h_ExecDemoStream {};
    SafetyHookInline h_VanimeAct {};
    SafetyHookInline h_StanimeAct {};

    void RunProc(int proc)
    {
        h_ExecProc.call<void*>(proc);
    }

    bool InSlowWindow(int frame)
    {
        for (const Window& w : kSlowWindows)
        {
            if (frame >= w.from && frame <= w.to)
            {
                return true;
            }
        }
        return false;
    }

    // Same call the script would have made, then the name so its 表示/非表示 messages arrive.
    void CreateFlag()
    {
        float pos[4] = { 0.0f, 0.0f, 0.0f, 1.0f };
        gFlag.work = NewPutVanimeObjectCalled(kFlagModel, 0, nullptr, nullptr, pos, kBaseFrames, kRandomBits, 0);
        if (!gFlag.work)
        {
            spdlog::warn("MGS 2: Ending Flag - could not create the flag.");
            return;
        }
        *reinterpret_cast<int*>(static_cast<uint8_t*>(gFlag.work) + kVanimeName) = kFlagName;
        gFlag.missed = 0;
        gStepFrame = -1;
        spdlog::info("MGS 2: Ending Flag - flag raised.");
    }

    // Turn a vector about an axis, so the cloth can swing on the pole like a hinge.
    void Hinge(float v[3], const float axis[3], float angle)
    {
        const float c = cosf(angle), s = sinf(angle);
        const float dot = axis[0] * v[0] + axis[1] * v[1] + axis[2] * v[2];
        const float cross[3] = {
            axis[1] * v[2] - axis[2] * v[1],
            axis[2] * v[0] - axis[0] * v[2],
            axis[0] * v[1] - axis[1] * v[0],
        };
        for (int i = 0; i < 3; i++)
        {
            v[i] = v[i] * c + cross[i] * s + axis[i] * dot * (1.0f - c);
        }
    }

    // Held at one shape, hung from its pole, and swung a few degrees on it like a hinge.
    void SwayPoleFlag(uint8_t* work, int index)
    {
        const PoleFlag& pole = kPoleFlags[index];
        const float period = static_cast<float>(kSwayTicks + (index * 29) % kSwaySpread);
        const float swing = 0.65f * sinf((gLastFrame + index * 53) * 2.0f * kPi / period)
            + 0.35f * sinf((gLastFrame + index * 31) * 2.0f * kPi / (period * kSwaySecond));
        const float yaw = pole.yaw * kPi / 180.0f;
        const float rise = kPoleRiseDeg * kPi / 180.0f;
        const float hang = kPoleHangMm / kModelClothMm;
        const float span = kPoleSpanMm / kModelHoistMm;
        const float out[3] = { cosf(yaw) * cosf(rise), sinf(rise), sinf(yaw) * cosf(rise) };
        float rows[3][3] = {
            { out[0] * kPlumbShear, -hang + out[1] * kPlumbShear, out[2] * kPlumbShear },   // falls plumb
            { out[0] * span, out[1] * span, out[2] * span },             // the top edge runs out along the pole
            { -sinf(yaw), 0.0f, cosf(yaw) },                             // keeps the turn a rotation, not a mirror
        };
        const float anchor[3] = {
            pole.x + rows[2][0] * kPoleSideMm,
            pole.y + kPoleLiftMm,
            pole.z + rows[2][2] * kPoleSideMm,
        };
        // Both top corners stay on the pole; only the hanging cloth swings.
        Hinge(rows[0], out, kSwayDeg * swing * kPi / 180.0f);
        Hinge(rows[2], out, kSwayDeg * swing * kPi / 180.0f);
        float* world = reinterpret_cast<float*>(*reinterpret_cast<uint8_t**>(work + kVanimeObjs));
        for (int r = 0; r < 3; r++)
        {
            for (int c = 0; c < 3; c++) { world[r * 4 + c] = rows[r][c]; }
            world[r * 4 + 3] = 0.0f;
        }
        for (int c = 0; c < 3; c++)
        {
            world[12 + c] = anchor[c] - (kModelPivot[0] * rows[0][c] + kModelPivot[1] * rows[1][c]
                + kModelPivot[2] * rows[2][c]);
        }
        world[15] = 1.0f;
    }

    void CreatePoleFlags()
    {
        int made = 0;
        for (int i = 0; i < kPoleCount; i++)
        {
            if (gPoles[i].work)
            {
                continue;
            }
            float pos[4] = { kPoleFlags[i].x, kPoleFlags[i].y, kPoleFlags[i].z, 1.0f };
            gPoles[i].work = NewPutVanimeObjectCalled(kFlagModel, 0, nullptr, nullptr, pos, kHeldFrames, 0, 0);
            if (!gPoles[i].work)
            {
                spdlog::warn("MGS 2: Ending Flag - pole flag {} was not created.", i);
                continue;
            }
            *reinterpret_cast<int*>(static_cast<uint8_t*>(gPoles[i].work) + kVanimeName) = kPoleName + i;
            gPoles[i].missed = 0;
            made++;
        }
        if (made)
        {
            spdlog::info("MGS 2: Ending Flag - {} flags on the street poles.", made);
        }
    }

    float StairFloor(float z)
    {
        if (z <= kLandingZ) { return kLandingY; }
        if (z >= kGroundZ) { return kGroundY; }
        return kLandingY + (kGroundY - kLandingY) * (z - kLandingZ) / (kGroundZ - kLandingZ);
    }

    int16_t* FallBuffer(uint8_t* work, int which)
    {
        return reinterpret_cast<int16_t**>(work + kStanimeBuffers)[which];
    }

    // The fall plays once and holds its last frame, which is the flag lying on Solidus. Settled, it is
    // run to that last frame at once and kept playing at rate zero so edited vertices still draw.
    void CreateFall(bool settled)
    {
        const float bound[4] = { 0.0f, 0.0f, 0.0f, 0.0f };
        gFall.work = NewPutSTanimeObjectCall(kFallName, kFallModel, nullptr, kFallPos, nullptr, nullptr, bound, bound, 0);
        if (!gFall.work)
        {
            spdlog::warn("MGS 2: Ending Flag - could not create the falling flag.");
            return;
        }
        uint8_t* work = static_cast<uint8_t*>(gFall.work);
        int& rate = *reinterpret_cast<int*>(work + kStanimeRate);
        int& mode = *reinterpret_cast<int*>(work + kStanimeMode);
        rate = kFullRate;
        if (settled)
        {
            for (int i = 0; i < kMaxSettleTicks && (mode & kStanimePlaying); i++)
            {
                h_StanimeAct.call<void>(work);
            }
            rate = 0;
            mode |= kStanimePlaying;
            const int verts = *reinterpret_cast<int*>(work + kStanimeVerts);
            gRest.assign(FallBuffer(work, 0), FallBuffer(work, 0) + verts * 3);
            std::copy(gRest.begin(), gRest.end(), FallBuffer(work, 1));
        }
        else
        {
            for (int i = 0; i < kFallSkip; i++)
            {
                h_StanimeAct.call<void>(work);
            }
            rate = kFallRate;
        }
        gFall.missed = 0;
        spdlog::info("MGS 2: Ending Flag - {}.", settled ? "flag lying on Solidus" : "flag falling");
    }

    // Pull the crumpled flag after the officer: the near edge follows him at once, the rest only once
    // the slack ahead of it is used up, all along his own path so it climbs the steps with him.
    void DragFlag()
    {
        if (!gFall.work || gDragDone || gRest.empty())
        {
            return;
        }
        void* officer = DM_GetObjectHandle(kOfficer);
        if (!officer)
        {
            return;
        }
        SignalFn signal = *reinterpret_cast<SignalFn*>(static_cast<uint8_t*>(officer) + kActorSignal);
        const float* matrix = reinterpret_cast<const float*>(signal(officer, kSignalGetMatrix, kRootJoint));
        if (!matrix)
        {
            return;
        }
        const float* root = matrix + kMatrixTrans;
        if (!gDragging)
        {
            gDragging = true;
            gStillFrames = 0;
            for (int i = 0; i < 3; i++) { gDragStart[i] = root[i]; gLastRoot[i] = root[i]; }
            spdlog::info("MGS 2: Ending Flag - officer takes the flag at frame {}.", gLastFrame);
            return;
        }
        float moved = 0.0f;
        for (int i = 0; i < 3; i++) { moved += fabsf(root[i] - gLastRoot[i]); gLastRoot[i] = root[i]; }
        gStillFrames = moved < kStillMm ? gStillFrames + 1 : 0;
        if (gStillFrames > kStillFrames)
        {
            gDragDone = true;
            spdlog::info("MGS 2: Ending Flag - officer stops at frame {}.", gLastFrame);
            return;
        }

        const float disp[3] = { root[0] - gDragStart[0], root[1] - gDragStart[1], root[2] - gDragStart[2] };
        const float dist = sqrtf(disp[0] * disp[0] + disp[2] * disp[2]);
        if (dist < kMinDragMm)
        {
            return;
        }
        const float dir[2] = { disp[0] / dist, disp[2] / dist };
        const size_t verts = gRest.size() / 3;
        float nearest = -FLT_MAX, farthest = FLT_MAX;
        for (size_t v = 0; v < verts; v++)
        {
            const float along = gRest[v * 3] * dir[0] + gRest[v * 3 + 2] * dir[1];
            nearest = std::max(nearest, along);
            farthest = std::min(farthest, along);
        }
        uint8_t* work = static_cast<uint8_t*>(gFall.work);
        int16_t* out[2] = { FallBuffer(work, 0), FallBuffer(work, 1) };
        for (size_t v = 0; v < verts; v++)
        {
            const float along = gRest[v * 3] * dir[0] + gRest[v * 3 + 2] * dir[1];
            const float slack = (nearest - along) * kSlackPerMm;
            const float pull = dist > slack ? (dist - slack) / dist : 0.0f;
            // The tail lays flat as it is dragged; the end in his hand stays bunched.
            const float rear = (nearest - along) / std::max(nearest - farthest, 1.0f);
            const float flat = std::max(0.0f, std::min((dist - slack) / kFlattenMm, 1.0f)) * rear;
            const float late = std::max(0.0f, std::min((dist - kDeflateStartMm) / kDeflateMm, 1.0f)) * rear;
            const float sag = flat + (1.0f - flat) * late;
            // Each vertex keeps its height over the step it rests on, then rides the steps it is pulled over.
            const float restZ = kFallPos[2] + gRest[v * 3 + 2];
            const float nowZ = restZ + pull * disp[2];
            const float height = (kFallPos[1] + gRest[v * 3 + 1] - StairFloor(restZ)) * (1.0f - sag);
            for (int i = 0; i < 3; i++)
            {
                float value = gRest[v * 3 + i] + pull * disp[i];
                if (i == 1)
                {
                    value = StairFloor(nowZ) + height - kFallPos[1];
                }
                out[0][v * 3 + i] = static_cast<int16_t>(value);
                out[1][v * 3 + i] = static_cast<int16_t>(value);
            }
        }
    }

    void Reset()
    {
        gDemo = Demo::Unknown;
        gLastFrame = -1;
        gStepFrame = -1;
        gFlag = Actor {};
        gFall = Actor {};
        for (Actor& pole : gPoles) { pole = Actor {}; }
        gRest.clear();
        gDragging = false;
        gDragDone = false;
    }

    void* ExecProc_hook(int proc)
    {
        if (g_GameVars.IsStage(MGS2Stages::D082P01) && gLastFrame <= 2)
        {
            if (proc == kProcFlagOff) { gDemo = gLastFrame == 0 ? Demo::Second : Demo::First; }
            if (proc == kProcVampOn) { gDemo = Demo::Third; }
        }
        return h_ExecProc.call<void*>(proc);
    }

    void ExecDemoStream_hook(void* work, uint8_t* stream, int exec)
    {
        if (!g_GameVars.IsStage(MGS2Stages::D082P01))
        {
            Reset();
            h_ExecDemoStream.call<void>(work, stream, exec);
            return;
        }

        const int frame = *reinterpret_cast<int*>(stream - 8) / 5;      // STREAM_TAG.time, 300 Hz
        if (frame < gLastFrame)
        {
            gDemo = Demo::Unknown;      // a new demo started
            gStepFrame = -1;
            gDragging = false;
            gDragDone = false;
        }
        gLastFrame = frame;
        gFlag.Check();
        gFall.Check();
        for (Actor& pole : gPoles) { pole.Check(); }
        h_ExecDemoStream.call<void>(work, stream, exec);    // its own proc calls tell us which demo this is

        if (gDemo == Demo::Unknown)
        {
            return;
        }
        if (frame == kSetupFrame)
        {
            CreatePoleFlags();
            if (gDemo == Demo::First)
            {
                if (!gFlag.work) { CreateFlag(); }
                RunProc(kProcFlagOn);
                RunProc(kProcFallenOff);
            }
            else
            {
                RunProc(kProcFlagOff);
                if (gDemo == Demo::Third && !gFall.work) { CreateFall(true); }
                if (!gFall.work) { RunProc(kProcFallenOn); }     // the flag stays where it fell
            }
        }
        if (gDemo == Demo::First && frame == kChopFrame)
        {
            RunProc(kProcFlagOff);
            if (!gFall.work) { CreateFall(false); }
        }
        if (gDemo == Demo::Third && frame >= kGrabFrame)
        {
            DragFlag();
        }
    }

    // The waving flag steps at the PS2's pace: hold its animation on the ticks in between.
    void VanimeAct_hook(uint8_t* work)
    {
        for (int i = 0; i < kPoleCount; i++)
        {
            if (gPoles[i].work != work)
            {
                continue;
            }
            gPoles[i].seen = true;
            h_VanimeAct.call<void>(work);       // keeps rebuilding the same shape, so it stays drawn
            SwayPoleFlag(work, i);
            return;
        }
        if (work != gFlag.work)
        {
            h_VanimeAct.call<void>(work);
            return;
        }
        gFlag.seen = true;
        const int stride = kStepFrames * (InSlowWindow(gLastFrame) ? kSlowFactor : 1);
        if (gLastFrame < 0 || gStepFrame < 0 || gLastFrame - gStepFrame >= stride)
        {
            gStepFrame = gLastFrame;
            h_VanimeAct.call<void>(work);
            return;
        }
        int& mode = *reinterpret_cast<int*>(work + kVanimeMode);
        const int saved = mode;
        mode = -1;
        h_VanimeAct.call<void>(work);
        mode = saved;
    }

    void StanimeAct_hook(uint8_t* work)
    {
        h_StanimeAct.call<void>(work);
        if (work == gFall.work)
        {
            gFall.seen = true;
        }
    }
}

void MGS2EndingFlag::Initialize()
{
    if (!(eGameType & MGS2) || !bEnabled)
    {
        return;
    }

    uint8_t* create = Memory::PatternScan(baseModule,
        "48 89 5C 24 ?? 48 89 6C 24 ?? 56 41 56 41 57 48 83 EC ?? 49 8B E9",
        "MGS 2: Ending Flag | put_vanime.c -> NewPutVanimeObjectCalled()");
    uint8_t* vanimeAct = Memory::PatternScan(baseModule,
        "40 53 48 83 EC ?? 48 8B D9 48 8D 54 24 ?? 8B 89 ?? ?? ?? ?? E8 ?? ?? ?? ?? 44 8B C8",
        "MGS 2: Ending Flag | put_vanime.c -> Act()");
    uint8_t* createFall = Memory::PatternScan(baseModule,
        "48 89 5C 24 ?? 48 89 6C 24 ?? 48 89 74 24 ?? 48 89 7C 24 ?? 41 56 48 83 EC ?? 49 8B F9 49 8B F0 45 33 C9 8B EA 44 8B F1 BA ?? ?? ?? ?? 41 B8 ?? ?? ?? ?? 41 8D 49 ?? E8 ?? ?? ?? ?? 48 8B D8 48 85 C0 0F 84 ?? ?? ?? ?? 4C 8D 05 ?? ?? ?? ?? 48 8B C8 48 8D 15 ?? ?? ?? ?? E8 ?? ?? ?? ?? 81 4B ?? ?? ?? ?? ?? 33 C0 48 89 43 ?? 4C 8B CE 48 89 43 ?? 44 8B C5 48 89 43 ?? 41 8B D6 48 8D 05 ?? ?? ?? ?? 48 8B CB 48 89 43 ?? 8B 84 24 ?? ?? ?? ?? 89 44 24 ?? 48 8B 84 24",
        "MGS 2: Ending Flag | put_stanime.c -> NewPutSTanimeObjectCall()");
    uint8_t* stanimeAct = Memory::PatternScan(baseModule,
        "40 53 48 83 EC ?? 48 89 6C 24 ?? 48 8D 54 24 ?? 48 89 74 24 ?? 48 8B D9 8B 49",
        "MGS 2: Ending Flag | put_stanime.c -> Act()");
    uint8_t* getHandle = Memory::PatternScan(baseModule,
        "40 53 48 83 EC ?? 8B D9 E8 ?? ?? ?? ?? 48 8B C8 48 85 C0 74 ?? 8B 49",
        "MGS 2: Ending Flag | demo_obj.c -> DM_GetObjectHandle()");
    uint8_t* execProc = Memory::PatternScan(baseModule,
        "40 53 48 83 EC ?? 8B D9 E8 ?? ?? ?? ?? 48 85 C0 74 ?? 33 D2",
        "MGS 2: Ending Flag | DM_ExecProc()");
    uint8_t* exec = Memory::PatternScan(baseModule,
        "48 89 5C 24 ?? 48 89 6C 24 ?? 48 89 74 24 ?? 57 48 83 EC ?? 33 C0 48 8D 2D",
        "MGS 2: Ending Flag | demo_pkt.c -> DM_ExecDemoStream()");
    if (!create || !vanimeAct || !createFall || !stanimeAct || !getHandle || !execProc || !exec)
    {
        return;
    }

    NewPutVanimeObjectCalled = reinterpret_cast<NewPutVanimeObjectCalledFn>(create);
    NewPutSTanimeObjectCall = reinterpret_cast<NewPutSTanimeObjectCallFn>(createFall);
    DM_GetObjectHandle = reinterpret_cast<GetObjectHandleFn>(getHandle);
    h_ExecProc = safetyhook::create_inline(reinterpret_cast<void*>(execProc), ExecProc_hook);
    LOG_HOOK(h_ExecProc, "MGS 2: Ending Flag | DM_ExecProc()")
    h_ExecDemoStream = safetyhook::create_inline(reinterpret_cast<void*>(exec), ExecDemoStream_hook);
    LOG_HOOK(h_ExecDemoStream, "MGS 2: Ending Flag | demo_pkt.c -> DM_ExecDemoStream()")
    h_VanimeAct = safetyhook::create_inline(reinterpret_cast<void*>(vanimeAct), VanimeAct_hook);
    LOG_HOOK(h_VanimeAct, "MGS 2: Ending Flag | put_vanime.c -> Act()")
    h_StanimeAct = safetyhook::create_inline(reinterpret_cast<void*>(stanimeAct), StanimeAct_hook);
    LOG_HOOK(h_StanimeAct, "MGS 2: Ending Flag | put_stanime.c -> Act()")
}
