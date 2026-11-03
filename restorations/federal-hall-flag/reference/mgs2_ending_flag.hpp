#pragma once

// MGS2 ending: put the American flag back on Federal Hall's pole. The script removed it
// before release and left the pole; the demos still call the flag's on/off procs.
namespace MGS2EndingFlag
{
    inline bool bEnabled = true;

    void Initialize();
}
