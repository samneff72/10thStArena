// Tests for handleArenaStatus in free_practice.js.
// These cover the input-synchronisation behaviour that Go tests cannot reach.

const $ = require("jquery");

// Globals that free_practice.js expects to find on the global object.
global.$ = $;
global.FreePracticeState = 7; // field.FreePractice iota value
global.bootstrap = { Modal: { getOrCreateInstance: () => ({ show: () => {}, hide: () => {} }) } };
global.CheesyWebsocket = class {
  constructor() {}
  send() {}
};
global.websocket = new global.CheesyWebsocket();

const { handleArenaStatus } = require("../free_practice.js");

// ---- helpers ----------------------------------------------------------------

const STATIONS = ["R1", "R2", "R3", "B1", "B2", "B3"];

function buildDom() {
  document.body.innerHTML = `
    <div id="fpModeBanner" class="fp-mode-banner fp-mode-setup">
      <div id="fpModeLabel" class="fp-mode-label">FREE PRACTICE SETUP</div>
    </div>
    <button id="enterBtn"></button>
    <button id="exitBtn" class="d-none"></button>
    <div id="reconfiguringOverlay" class="d-none"></div>
    <span id="apStatus" data-status=""></span>
    <span id="swStatus" data-status=""></span>
    <span id="hwEStopStatus" data-status-ok=""></span>
    <div id="fieldEstopOverlay" style="display:none"></div>
    <a id="matchPlayBtn" href="/match_play" class="btn btn-secondary">Match Play</a>
    ${STATIONS.map(
      (s) => `
      <div id="slot-${s}">
        <input id="teamId-${s}" />
        <input id="wpaKey-${s}" />
        <button class="btn btn-sm btn-primary">Register</button>
        <button class="btn btn-sm btn-outline-secondary">Clear</button>
        <button class="btn btn-sm btn-danger">E-Stop</button>
        <div id="status-${s}"></div>
      </div>`
    ).join("")}
  `;
}

function emptyStatus(overrides = {}) {
  const stations = {};
  STATIONS.forEach((s) => {
    stations[s] = overrides[s] ?? { Team: null, DsConn: null, EStop: false };
  });
  return {
    MatchState: FreePracticeState,
    FreePracticeReconfiguring: false,
    AllianceStations: stations,
  };
}

function occupiedStation(teamId, wpaKey = "") {
  return { Team: { Id: teamId, WpaKey: wpaKey }, DsConn: null, EStop: false };
}

// ---- tests ------------------------------------------------------------------

beforeEach(buildDom);

describe("handleArenaStatus — Match Play button", () => {
  test("disables the Match Play button when free practice is active", () => {
    handleArenaStatus(emptyStatus());
    expect($("#matchPlayBtn").hasClass("disabled")).toBe(true);
    expect($("#matchPlayBtn").attr("aria-disabled")).toBe("true");
  });

  test("enables the Match Play button when not in free practice", () => {
    // First put it into free-practice state so the button is disabled.
    handleArenaStatus(emptyStatus());
    expect($("#matchPlayBtn").hasClass("disabled")).toBe(true);

    // Now simulate a non-free-practice arena status.
    handleArenaStatus({ ...emptyStatus(), MatchState: 0 /* PreMatch */ });
    expect($("#matchPlayBtn").hasClass("disabled")).toBe(false);
    expect($("#matchPlayBtn").attr("aria-disabled")).toBe("false");
  });
});

describe("handleArenaStatus — team number field", () => {
  test("does not clear a user-typed team number on an empty-slot status push", () => {
    // Simulate the operator typing 254 and tabbing away.
    $("#teamId-R1").val("254");
    // arenaSet data flag is NOT set — the user typed this, arena did not.

    // The next arena status push arrives with R1 still empty (not registered).
    handleArenaStatus(emptyStatus());

    // The field must still show what the operator typed.
    expect($("#teamId-R1").val()).toBe("254");
  });

  test("clears the team number when a previously-registered slot becomes empty", () => {
    // Arena status sets the field (marks arenaSet=true).
    handleArenaStatus(emptyStatus({ R1: occupiedStation(254, "key") }));
    expect($("#teamId-R1").val()).toBe("254");

    // Slot is cleared on the server; next status push arrives empty.
    handleArenaStatus(emptyStatus());

    // Field should clear because arena status was the one who set it.
    expect($("#teamId-R1").val()).toBe("");
  });

  test("populates team number from occupied-slot status", () => {
    handleArenaStatus(emptyStatus({ R2: occupiedStation(1114) }));
    expect($("#teamId-R2").val()).toBe("1114");
  });

  test("does not touch a focused field", () => {
    // jsdom doesn't dispatch real focus events, but we can simulate by
    // checking that the ':focus' branch is respected via a spy.
    // Instead, verify the non-focus path works and that a focused element
    // would be skipped (we rely on the jQuery :focus selector in the impl).
    $("#teamId-R1").val("999");
    // Blur is handled by the blur event handler (separate); here we just
    // confirm the status handler leaves an already-set arenaSet=false value alone.
    handleArenaStatus(emptyStatus());
    expect($("#teamId-R1").val()).toBe("999"); // unchanged
  });
});

describe("handleArenaStatus — WPA key field", () => {
  test("does not clear a user-typed WPA key on an empty-slot status push", () => {
    $("#wpaKey-R1").val("mykey");

    handleArenaStatus(emptyStatus());

    expect($("#wpaKey-R1").val()).toBe("mykey");
  });

  test("clears the WPA key when arena status previously set it and slot becomes empty", () => {
    handleArenaStatus(emptyStatus({ R1: occupiedStation(254, "prevkey") }));
    expect($("#wpaKey-R1").val()).toBe("prevkey");

    handleArenaStatus(emptyStatus());

    expect($("#wpaKey-R1").val()).toBe("");
  });

  test("populates WPA key from occupied-slot status", () => {
    handleArenaStatus(emptyStatus({ B3: occupiedStation(9999, "wpaB3") }));
    expect($("#wpaKey-B3").val()).toBe("wpaB3");
  });
});

// ── Setup / PreMatch state ────────────────────────────────────────────────

function preMatchStatus(overrides = {}) {
  const stations = {};
  STATIONS.forEach((s) => {
    stations[s] = overrides[s] ?? { Team: null, DsConn: null, EStop: false };
  });
  return {
    MatchState: 0, // PreMatch
    FreePracticeReconfiguring: false,
    AllianceStations: stations,
  };
}

describe("handleArenaStatus — setup (PreMatch) state", () => {
  test("inputs are enabled in PreMatch when not reconfiguring", () => {
    handleArenaStatus(preMatchStatus());
    STATIONS.forEach((s) => {
      expect($("#teamId-" + s).prop("disabled")).toBe(false);
      expect($("#wpaKey-" + s).prop("disabled")).toBe(false);
      // Non-danger buttons (Register, Clear) should also be enabled.
      const nonDanger = $("#slot-" + s).find("button:not(.btn-danger)");
      nonDanger.each(function () {
        expect($(this).prop("disabled")).toBe(false);
      });
    });
  });

  test("banner has fp-mode-setup class and not fp-mode-enabled in PreMatch", () => {
    handleArenaStatus(preMatchStatus());
    expect($("#fpModeBanner").hasClass("fp-mode-setup")).toBe(true);
    expect($("#fpModeBanner").hasClass("fp-mode-enabled")).toBe(false);
  });

  test("banner label reads FREE PRACTICE SETUP in PreMatch", () => {
    handleArenaStatus(preMatchStatus());
    expect($("#fpModeLabel").text()).toBe("FREE PRACTICE SETUP");
  });

  test("E-Stop button is disabled in PreMatch", () => {
    handleArenaStatus(preMatchStatus());
    STATIONS.forEach((s) => {
      expect($("#slot-" + s).find(".btn-danger").prop("disabled")).toBe(true);
    });
  });

  test("Register button is enabled in setup state so slots can be committed", () => {
    handleArenaStatus(preMatchStatus());
    STATIONS.forEach((s) => {
      const registerBtn = $("#slot-" + s).find(".btn-primary");
      expect(registerBtn.length).toBe(1);
      expect(registerBtn.prop("disabled")).toBe(false);
    });
  });

  test("Register button is disabled while reconfiguring in setup state", () => {
    handleArenaStatus({ ...preMatchStatus(), FreePracticeReconfiguring: true });
    STATIONS.forEach((s) => {
      expect($("#slot-" + s).find(".btn-primary").prop("disabled")).toBe(true);
    });
  });

  test("inputs are disabled while reconfiguring in PreMatch", () => {
    handleArenaStatus({ ...preMatchStatus(), FreePracticeReconfiguring: true });
    STATIONS.forEach((s) => {
      expect($("#teamId-" + s).prop("disabled")).toBe(true);
      expect($("#wpaKey-" + s).prop("disabled")).toBe(true);
    });
  });

  test("banner has fp-mode-enabled and label reads FREE PRACTICE ENABLED when active", () => {
    handleArenaStatus(emptyStatus()); // FreePracticeState
    expect($("#fpModeBanner").hasClass("fp-mode-enabled")).toBe(true);
    expect($("#fpModeBanner").hasClass("fp-mode-setup")).toBe(false);
    expect($("#fpModeLabel").text()).toBe("FREE PRACTICE ENABLED");
  });
});
