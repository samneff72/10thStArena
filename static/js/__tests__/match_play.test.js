// Tests for handleArenaStatus in match_play.js.
// Covers button-state transitions and bypass-checkbox sync that Go tests
// cannot reach.

const $ = require("jquery");

global.$ = $;

// Globals that match_play.js (and match_timing.js) expect on the global object.
global.matchStates = {
  0: "PRE_MATCH",
  1: "START_MATCH",
  2: "WARMUP_PERIOD",
  3: "AUTO_PERIOD",
  4: "PAUSE_PERIOD",
  5: "TELEOP_PERIOD",
  6: "POST_MATCH",
  7: "FREE_PRACTICE",
};
global.matchTypeTest = 0;
global.matchTiming = {
  WarmupDurationSec: 3,
  AutoDurationSec: 15,
  PauseDurationSec: 2,
  TeleopDurationSec: 140,
};
global.translateMatchTime = function (data, cb) {
  cb(global.matchStates[data.MatchState], "", 0);
};
global.handleMatchTiming = function () {};
global.getCountdownString = function (s) {
  return String(s);
};
global.CheesyWebsocket = class {
  constructor() {}
  send() {}
};
global.websocket = new global.CheesyWebsocket();

const { handleArenaStatus, handleMatchLoad } = require("../match_play.js");

// ---- helpers ----------------------------------------------------------------

const STATIONS = ["R1", "R2", "R3", "B1", "B2", "B3"];

function buildDom() {
  document.body.innerHTML = `
    <div id="fieldEstopOverlay" style="display:none"></div>
    <div id="timerBand"></div>
    <span id="periodText"></span>
    <span id="timerText"></span>
    <button id="btnStart" disabled></button>
    <button id="btnAbort" disabled></button>
    <button id="btnClear" disabled></button>
    <input type="checkbox" id="muteMatchSounds">
    <strong id="matchName"></strong>
    <span id="testMatchNameWrap" style="display:none"></span>
    <input type="text" id="testMatchName">
    ${STATIONS.map(
      (s) => `
      <div id="card-${s}">
        <span id="ds-${s}">—</span>
        <button id="estop-${s}" data-station="${s}">E-STOP</button>
        <input type="checkbox" id="bypass-${s}">
        <input type="number" id="team-${s}">
      </div>`
    ).join("")}
    <button id="btnRegister" disabled></button>
  `;
}

function emptyStations(overrides = {}) {
  const s = {};
  STATIONS.forEach((st) => {
    s[st] = overrides[st] ?? { DsConn: null, EStop: false, AStop: false, Bypass: false, Team: null };
  });
  return s;
}

function makeStatus(matchState, canStart, stationOverrides = {}) {
  return {
    MatchState: matchState,
    CanStartMatch: canStart,
    GpioFieldEStopActive: false,
    AllianceStations: emptyStations(stationOverrides),
  };
}

// ---- setup ------------------------------------------------------------------

beforeEach(buildDom);

// ---- Start button -----------------------------------------------------------

describe("handleArenaStatus — Start button", () => {
  test("disabled when PRE_MATCH and CanStartMatch=false", () => {
    handleArenaStatus(makeStatus(0 /* PRE_MATCH */, false));
    expect(document.getElementById("btnStart").disabled).toBe(true);
  });

  test("enabled when PRE_MATCH and CanStartMatch=true", () => {
    handleArenaStatus(makeStatus(0 /* PRE_MATCH */, true));
    expect(document.getElementById("btnStart").disabled).toBe(false);
  });

  test("disabled when START_MATCH regardless of CanStartMatch", () => {
    handleArenaStatus(makeStatus(1 /* START_MATCH */, true));
    expect(document.getElementById("btnStart").disabled).toBe(true);
  });

  test("disabled when WARMUP_PERIOD", () => {
    handleArenaStatus(makeStatus(2 /* WARMUP_PERIOD */, true));
    expect(document.getElementById("btnStart").disabled).toBe(true);
  });

  test("disabled when AUTO_PERIOD", () => {
    handleArenaStatus(makeStatus(3 /* AUTO_PERIOD */, false));
    expect(document.getElementById("btnStart").disabled).toBe(true);
  });

  test("disabled when POST_MATCH", () => {
    handleArenaStatus(makeStatus(6 /* POST_MATCH */, false));
    expect(document.getElementById("btnStart").disabled).toBe(true);
  });

  test("transitions: enabled in PRE_MATCH then disabled once match starts", () => {
    handleArenaStatus(makeStatus(0, true));
    expect(document.getElementById("btnStart").disabled).toBe(false);

    handleArenaStatus(makeStatus(1 /* START_MATCH */, false));
    expect(document.getElementById("btnStart").disabled).toBe(true);
  });
});

// ---- Abort button -----------------------------------------------------------

describe("handleArenaStatus — Abort button", () => {
  test("disabled in PRE_MATCH", () => {
    handleArenaStatus(makeStatus(0, true));
    expect(document.getElementById("btnAbort").disabled).toBe(true);
  });

  test("enabled in START_MATCH", () => {
    handleArenaStatus(makeStatus(1, false));
    expect(document.getElementById("btnAbort").disabled).toBe(false);
  });

  test("enabled in WARMUP_PERIOD", () => {
    handleArenaStatus(makeStatus(2, false));
    expect(document.getElementById("btnAbort").disabled).toBe(false);
  });

  test("enabled in AUTO_PERIOD", () => {
    handleArenaStatus(makeStatus(3, false));
    expect(document.getElementById("btnAbort").disabled).toBe(false);
  });

  test("enabled in TELEOP_PERIOD", () => {
    handleArenaStatus(makeStatus(5, false));
    expect(document.getElementById("btnAbort").disabled).toBe(false);
  });

  test("disabled in POST_MATCH", () => {
    handleArenaStatus(makeStatus(6, false));
    expect(document.getElementById("btnAbort").disabled).toBe(true);
  });
});

// ---- Clear Match button -----------------------------------------------

describe("handleArenaStatus — Clear Match button", () => {
  test("disabled in PRE_MATCH", () => {
    handleArenaStatus(makeStatus(0, false));
    expect(document.getElementById("btnClear").disabled).toBe(true);
  });

  test("disabled while match is in progress", () => {
    for (const state of [1, 2, 3, 4, 5]) {
      handleArenaStatus(makeStatus(state, false));
      expect(document.getElementById("btnClear").disabled).toBe(true);
    }
  });

  test("enabled in POST_MATCH", () => {
    handleArenaStatus(makeStatus(6, false));
    expect(document.getElementById("btnClear").disabled).toBe(false);
  });
});

// ---- Bypass checkbox sync ---------------------------------------------------

describe("handleArenaStatus — bypass checkbox sync", () => {
  test("sets checkbox to true when server reports Bypass=true", () => {
    handleArenaStatus(
      makeStatus(0, false, { R1: { DsConn: null, EStop: false, AStop: false, Bypass: true, Team: null } })
    );
    expect(document.getElementById("bypass-R1").checked).toBe(true);
  });

  test("sets checkbox to false when server reports Bypass=false", () => {
    // Pre-check the box to make sure it gets un-checked.
    document.getElementById("bypass-B2").checked = true;
    handleArenaStatus(
      makeStatus(0, false, { B2: { DsConn: null, EStop: false, AStop: false, Bypass: false, Team: null } })
    );
    expect(document.getElementById("bypass-B2").checked).toBe(false);
  });

  test("all six stations sync independently", () => {
    const overrides = {};
    STATIONS.forEach((s, i) => {
      overrides[s] = { DsConn: null, EStop: false, AStop: false, Bypass: i % 2 === 0, Team: null };
    });
    handleArenaStatus(makeStatus(0, false, overrides));
    STATIONS.forEach((s, i) => {
      expect(document.getElementById("bypass-" + s).checked).toBe(i % 2 === 0);
    });
  });
});

// ---- DS connection badge ----------------------------------------------------

describe("handleArenaStatus — DS connection badge", () => {
  test("shows 'No DS' when DsConn is null", () => {
    handleArenaStatus(makeStatus(0, false));
    expect(document.getElementById("ds-R1").textContent).toBe("No DS");
  });

  test("shows 'No DS' when DsLinked is false", () => {
    handleArenaStatus(
      makeStatus(0, false, {
        R1: { DsConn: { DsLinked: false, RobotLinked: false, BatteryVoltage: 0 }, EStop: false, AStop: false, Bypass: false, Team: null },
      })
    );
    expect(document.getElementById("ds-R1").textContent).toBe("No DS");
  });

  test("shows battery voltage when DS is linked", () => {
    handleArenaStatus(
      makeStatus(0, false, {
        R2: { DsConn: { DsLinked: true, RobotLinked: true, BatteryVoltage: 12.3 }, EStop: false, AStop: false, Bypass: false, Team: null },
      })
    );
    expect(document.getElementById("ds-R2").textContent).toBe("12.3V");
  });
});

// ---- E-Stop state on card ---------------------------------------------------

describe("handleArenaStatus — E-Stop card state", () => {
  test("sets data-estop=true when station is e-stopped", () => {
    handleArenaStatus(
      makeStatus(0, false, {
        B1: { DsConn: null, EStop: true, AStop: false, Bypass: false, Team: null },
      })
    );
    expect(document.getElementById("card-B1").dataset.estop).toBe("true");
    expect(document.getElementById("estop-B1").textContent).toBe("UN-STOP");
  });

  test("sets data-estop=false and button text E-STOP when not e-stopped", () => {
    handleArenaStatus(makeStatus(0, false));
    expect(document.getElementById("card-R3").dataset.estop).toBe("false");
    expect(document.getElementById("estop-R3").textContent).toBe("E-STOP");
  });
});

// ---- Field E-Stop overlay ---------------------------------------------------

describe("handleArenaStatus — Field E-Stop overlay", () => {
  test("overlay hidden when GpioFieldEStopActive=false", () => {
    handleArenaStatus(makeStatus(0, false));
    expect(document.getElementById("fieldEstopOverlay").style.display).toBe("none");
  });

  test("overlay shown when GpioFieldEStopActive=true", () => {
    handleArenaStatus({ ...makeStatus(0, false), GpioFieldEStopActive: true });
    expect(document.getElementById("fieldEstopOverlay").style.display).toBe("flex");
  });
});

// ---- handleMatchLoad --------------------------------------------------------

describe("handleMatchLoad", () => {
  test("displays match name", () => {
    handleMatchLoad({
      Match: { LongName: "Test Match", Type: 0 },
      AllowSubstitution: true,
      IsReplay: false,
      Teams: { R1: null, R2: null, R3: null, B1: null, B2: null, B3: null },
    });
    expect(document.getElementById("matchName").textContent).toBe("Test Match");
  });

  test("populates team inputs for occupied stations", () => {
    handleMatchLoad({
      Match: { LongName: "Q1", Type: 2 },
      AllowSubstitution: false,
      IsReplay: false,
      Teams: {
        R1: { Id: 254 }, R2: null, R3: null,
        B1: { Id: 1114 }, B2: null, B3: null,
      },
    });
    expect(document.getElementById("team-R1").value).toBe("254");
    expect(document.getElementById("team-B1").value).toBe("1114");
    expect(document.getElementById("team-R2").value).toBe("");
  });

  test("disables team inputs when AllowSubstitution=false", () => {
    handleMatchLoad({
      Match: { LongName: "Q1", Type: 2 },
      AllowSubstitution: false,
      IsReplay: false,
      Teams: { R1: null, R2: null, R3: null, B1: null, B2: null, B3: null },
    });
    STATIONS.forEach((s) => {
      expect(document.getElementById("team-" + s).disabled).toBe(true);
    });
  });

  test("enables team inputs when AllowSubstitution=true", () => {
    handleMatchLoad({
      Match: { LongName: "Test Match", Type: 0 },
      AllowSubstitution: true,
      IsReplay: false,
      Teams: { R1: null, R2: null, R3: null, B1: null, B2: null, B3: null },
    });
    STATIONS.forEach((s) => {
      expect(document.getElementById("team-" + s).disabled).toBe(false);
    });
  });

  test("shows test match name input only for Test matches", () => {
    handleMatchLoad({
      Match: { LongName: "Test Match", Type: 0 /* matchTypeTest */ },
      AllowSubstitution: true,
      IsReplay: false,
      Teams: { R1: null, R2: null, R3: null, B1: null, B2: null, B3: null },
    });
    expect(document.getElementById("testMatchNameWrap").style.display).not.toBe("none");

    handleMatchLoad({
      Match: { LongName: "Q1", Type: 2 },
      AllowSubstitution: false,
      IsReplay: false,
      Teams: { R1: null, R2: null, R3: null, B1: null, B2: null, B3: null },
    });
    expect(document.getElementById("testMatchNameWrap").style.display).toBe("none");
  });
});
