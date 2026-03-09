// Copyright 2014 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Client-side logic for the match play page (practice field controller).

var websocket;
const estopTimers = {};
const stations = ["R1", "R2", "R3", "B1", "B2", "B3"];

// --- E-Stop hold-to-confirm (600 ms) ---

const estopStart = function (btn) {
  const station = btn.dataset.station;
  btn.classList.add("estop-holding");
  estopTimers[station] = setTimeout(function () {
    websocket.send("toggleEStop", station);
    btn.classList.remove("estop-holding");
  }, 600);
};

const estopCancel = function (btn) {
  const station = btn.dataset.station;
  clearTimeout(estopTimers[station]);
  btn.classList.remove("estop-holding");
};

// --- Match control commands ---

const loadMatch = function (matchId) {
  websocket.send("loadMatch", {matchId: matchId});
};

const substituteTeams = function () {
  const teams = {
    Red1: getTeamNumber("R1"),
    Red2: getTeamNumber("R2"),
    Red3: getTeamNumber("R3"),
    Blue1: getTeamNumber("B1"),
    Blue2: getTeamNumber("B2"),
    Blue3: getTeamNumber("B3"),
  };
  websocket.send("substituteTeams", teams);
  document.getElementById("btnSubstitute").disabled = true;
};

const markSubstitution = function () {
  document.getElementById("btnSubstitute").disabled = false;
};

const toggleBypass = function (station) {
  websocket.send("toggleBypass", station);
};

// --- Field E-Stop overlay ---

const clearFieldEStop = function () {
  websocket.send("clearFieldEStop", null);
};

const startMatch = function () {
  const mute = document.getElementById("muteMatchSounds").checked;
  websocket.send("startMatch", {muteMatchSounds: mute});
};

const abortMatch = function () {
  websocket.send("abortMatch");
};

const commitResults = function () {
  websocket.send("commitResults");
};

const discardResults = function () {
  if (confirm("Discard match results and load next match?")) {
    websocket.send("discardResults");
  }
};

const setTestMatchName = function () {
  websocket.send("setTestMatchName", document.getElementById("testMatchName").value);
};

const getTeamNumber = function (station) {
  const val = document.getElementById("team-" + station).value.trim();
  return val ? parseInt(val) : 0;
};

// --- WebSocket message handlers ---

const handleArenaStatus = function (data) {
  for (const station of stations) {
    const st = data.AllianceStations[station];
    if (!st) continue;

    const card = document.getElementById("card-" + station);
    const dsEl = document.getElementById("ds-" + station);
    const estopBtn = document.getElementById("estop-" + station);
    const bypassChk = document.getElementById("bypass-" + station);

    // DS / Robot status badge.
    if (st.DsConn && st.DsConn.DsLinked) {
      const v = st.DsConn.BatteryVoltage.toFixed(1) + "V";
      dsEl.textContent = v;
      dsEl.dataset.ok = st.DsConn.RobotLinked ? "true" : "mid";
    } else {
      dsEl.textContent = "No DS";
      dsEl.dataset.ok = "false";
    }

    // E-Stop state — card pulses red, button turns green to show it is active.
    card.dataset.estop = st.EStop ? "true" : "false";
    card.dataset.astop = st.AStop ? "true" : "false";
    estopBtn.dataset.active = st.EStop ? "true" : "false";
    estopBtn.textContent = st.EStop ? "UN-STOP" : "E-STOP";

    // Bypass checkbox.
    bypassChk.checked = st.Bypass;
  }

  // Field e-stop overlay — blocks all controls when hardware e-stop is active.
  document.getElementById("fieldEstopOverlay").style.display =
    data.GpioFieldEStopActive ? "flex" : "none";

  // Update control button states.
  const btnStart = document.getElementById("btnStart");
  const btnAbort = document.getElementById("btnAbort");
  const btnCommit = document.getElementById("btnCommit");
  const btnDiscard = document.getElementById("btnDiscard");

  switch (matchStates[data.MatchState]) {
    case "PRE_MATCH":
      btnStart.disabled = !data.CanStartMatch;
      btnAbort.disabled = true;
      btnCommit.disabled = true;
      btnDiscard.disabled = true;
      break;
    case "START_MATCH":
    case "WARMUP_PERIOD":
    case "AUTO_PERIOD":
    case "PAUSE_PERIOD":
    case "TELEOP_PERIOD":
      btnStart.disabled = true;
      btnAbort.disabled = false;
      btnCommit.disabled = true;
      btnDiscard.disabled = true;
      break;
    case "POST_MATCH":
      btnStart.disabled = true;
      btnAbort.disabled = true;
      btnCommit.disabled = false;
      btnDiscard.disabled = false;
      break;
    default:
      // FREE_PRACTICE or unknown.
      btnStart.disabled = true;
      btnAbort.disabled = true;
      btnCommit.disabled = true;
      btnDiscard.disabled = true;
  }
};

const handleMatchLoad = function (data) {
  document.getElementById("matchName").textContent = data.Match.LongName;
  document.getElementById("testMatchName").value = data.Match.LongName;
  document.getElementById("testMatchNameWrap").style.display =
    data.Match.Type === matchTypeTest ? "" : "none";

  for (const station of stations) {
    const team = data.Teams[station];
    const input = document.getElementById("team-" + station);
    input.value = team ? team.Id : "";
    input.disabled = !data.AllowSubstitution;
  }
  document.getElementById("btnSubstitute").disabled = true;

  // Refresh the match list sidebar.
  fetch("/match_play/match_load")
    .then(function (r) { return r.text(); })
    .then(function (html) { document.getElementById("matchListColumn").innerHTML = html; });
};

const handleMatchTime = function (data) {
  translateMatchTime(data, function (state, stateText, countdown) {
    document.getElementById("periodText").textContent = stateText || "—";
    const secs = Math.max(0, countdown);
    document.getElementById("timerText").textContent =
      (state === "POST_MATCH" || state === "FREE_PRACTICE") ? "—" : getCountdownString(secs);

    // Period colour coding on the timer band.
    const band = document.getElementById("timerBand");
    switch (state) {
      case "AUTO_PERIOD":
        band.dataset.period = "auto";
        break;
      case "TELEOP_PERIOD":
        band.dataset.period = "teleop";
        break;
      case "PAUSE_PERIOD":
        band.dataset.period = "pause";
        break;
      case "WARMUP_PERIOD":
      case "START_MATCH":
        band.dataset.period = "warmup";
        break;
      default:
        band.dataset.period = "";
    }
  });
};

$(function () {
  websocket = new CheesyWebsocket("/match_play/websocket", {
    arenaStatus: function (event) { handleArenaStatus(event.data); },
    matchLoad:   function (event) { handleMatchLoad(event.data); },
    matchTime:   function (event) { handleMatchTime(event.data); },
    matchTiming: function (event) { handleMatchTiming(event.data); },
  });
});
