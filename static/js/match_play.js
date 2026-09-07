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

const registerTeams = function () {
  const teams = {
    Red1: getTeamNumber("R1"),
    Red2: getTeamNumber("R2"),
    Red3: getTeamNumber("R3"),
    Blue1: getTeamNumber("B1"),
    Blue2: getTeamNumber("B2"),
    Blue3: getTeamNumber("B3"),
    // Sent alongside the numbers so one Register does both: the key is what the radio is
    // configured with, and a station registered without one has no working WiFi. Blank
    // leaves whatever key the team already has rather than clearing it.
    WpaKeys: {
      R1: getWpaKey("R1"),
      R2: getWpaKey("R2"),
      R3: getWpaKey("R3"),
      B1: getWpaKey("B1"),
      B2: getWpaKey("B2"),
      B3: getWpaKey("B3"),
    },
  };
  websocket.send("registerTeams", teams);
  document.getElementById("btnRegister").disabled = true;
};

const markRegistration = function () {
  document.getElementById("btnRegister").disabled = false;
};

const toggleBypass = function (station) {
  websocket.send("toggleBypass", station);
};

// --- Field E-Stop overlay ---

const clearFieldEStop = function () {
  websocket.send("clearFieldEStop", null);
};

// Pushed on every toggle so the control means something before a match starts -- an abort
// cue should follow what the box says right now, not what it said at the last start.
const setMuteMatchSounds = function () {
  const mute = document.getElementById("muteMatchSounds").checked;
  websocket.send("setMuteMatchSounds", {muteMatchSounds: mute});
};

const startMatch = function () {
  const mute = document.getElementById("muteMatchSounds").checked;
  websocket.send("startMatch", {muteMatchSounds: mute});
};

const abortMatch = function () {
  websocket.send("abortMatch");
};

const clearMatch = function () {
  websocket.send("clearMatch");
};

// Bypasses every station with no team registered, so a 1v0 practice match can start
// without ticking five checkboxes. Occupied stations are left alone.
const bypassEmptyStations = function () {
  websocket.send("bypassEmptyStations");
};

// Selects how the AUTO result is decided for the next match. Rejected by the server
// once a match is underway, since the winner drives both the HUB lighting and the game
// data already sent to driver stations.
const setAutoWinnerMode = function () {
  websocket.send("setAutoWinnerMode", document.getElementById("autoWinnerMode").value);
};

const setTestMatchName = function () {
  websocket.send("setTestMatchName", document.getElementById("testMatchName").value);
};

// Looks the entered team up and fills in its WPA key, or offers to create it.
//
// Registering rejects a team the database has never seen -- validateTeams returns "Team N
// is not present at the event" -- so without the offer the operator is told no and has to
// leave for Setup > Teams and come back. Same endpoints Free Practice uses.
const lookUpTeam = function (station) {
  const teamId = getTeamNumber(station);
  const wpa = document.getElementById("wpaKey-" + station);
  if (!teamId || teamId < 1) {
    // Clearing the number clears a key that came from the database, but never one the
    // operator typed themselves.
    if (wpa.dataset.autofilled === "true") {
      wpa.value = "";
      delete wpa.dataset.autofilled;
    }
    return;
  }

  fetch("/setup/teams/" + teamId)
    .then(function (response) {
      if (response.ok) {
        return response.json().then(function (data) {
          // A key the operator typed is left alone: they may be deliberately changing it,
          // and overwriting what someone just entered is worse than leaving a stale one.
          if (wpa.value === "" || wpa.dataset.autofilled === "true") {
            wpa.value = data.wpaKey || "";
            wpa.dataset.autofilled = "true";
          }
        });
      }
      if (response.status === 404) {
        showTeamNotInDbModal(teamId, station);
      }
    })
    .catch(function () {});
};

// Offers to create a team the database does not have, so registration can proceed without
// leaving the page.
const showTeamNotInDbModal = function (teamId, station) {
  document.getElementById("teamNotInDbMessage").textContent =
    "Team " + teamId + " is not in the database. Add it now, or clear the number.";

  const modalEl = document.getElementById("teamNotInDbModal");
  const modal = bootstrap.Modal.getOrCreateInstance(modalEl);

  // Rebound each time, so the handlers close over the current team and station.
  const cancel = document.getElementById("teamNotInDbCancel");
  const add = document.getElementById("teamNotInDbAdd");
  cancel.onclick = function () {
    document.getElementById("team-" + station).value = "";
    markRegistration();
    modal.hide();
  };
  add.onclick = function () {
    fetch("/setup/teams/quick-add", {
      method: "POST",
      headers: {"Content-Type": "application/x-www-form-urlencoded"},
      body: "id=" + teamId,
    })
      .then(function (response) {
        if (response.ok) {
          modal.hide();
          // Created with a default key, so pull it straight back in rather than leaving
          // the operator to wonder what it was given.
          lookUpTeam(station);
        } else {
          response.text().then(function (msg) { alert("Failed to add team: " + msg); });
        }
      })
      .catch(function () { alert("Failed to add team."); });
  };

  modal.show();
};

const getWpaKey = function (station) {
  return document.getElementById("wpaKey-" + station).value.trim();
};

// Typing in the key box makes it the operator's, so a later team lookup will not overwrite
// it. markRegistration is called separately by the field's own onchange.
const markWpaKeyEdited = function (station) {
  delete document.getElementById("wpaKey-" + station).dataset.autofilled;
};

const getTeamNumber = function (station) {
  const val = document.getElementById("team-" + station).value.trim();
  return val ? parseInt(val) : 0;
};

// --- WebSocket message handlers ---

// Set once a navigation is under way, so the broadcasts that arrive before the page
// unloads do not each trigger another one.
let leavingForOtherView = false;

const handleArenaStatus = function (data) {
  // Every kiosk shows the same operating page. The server records whichever of them was
  // opened most recently, and displays sitting on the other one follow, so a field with
  // several screens does not have them disagreeing about what is being run.
  if (data.CurrentView && data.CurrentView !== "match_play" && !leavingForOtherView) {
    leavingForOtherView = true;
    window.location.href = "/" + data.CurrentView;
    return;
  }

  for (const station of stations) {
    const st = data.AllianceStations[station];
    if (!st) continue;

    const card = document.getElementById("card-" + station);
    const dsEl = document.getElementById("ds-" + station);
    const estopBtn = document.getElementById("estop-" + station);
    const bypassChk = document.getElementById("bypass-" + station);

    // DS / Robot status badge.
    //
    // Port link is the fallback, and the only thing that sees an unregistered station at
    // all: with no team there is no subnet, so a laptop gets no address and never connects.
    // The switch reports the cable regardless of any of that.
    const registered = Boolean(st.Team && st.Team.Id);
    if (st.DsConn && st.DsConn.DsLinked) {
      const v = st.DsConn.BatteryVoltage.toFixed(1) + "V";
      dsEl.textContent = v;
      dsEl.dataset.ok = st.DsConn.RobotLinked ? "true" : "mid";
      dsEl.title = "";
    } else if (data.StationLinksKnown && st.PortLinked && !registered) {
      dsEl.textContent = "CABLE";
      dsEl.dataset.ok = "mid";
      dsEl.title = "Something is plugged into this port, but no team is registered here";
    } else if (data.StationLinksKnown && registered && !st.PortLinked) {
      dsEl.textContent = "No cable";
      dsEl.dataset.ok = "false";
      dsEl.title = "A team is registered here but nothing is plugged into the port";
    } else {
      dsEl.textContent = "No DS";
      dsEl.dataset.ok = "false";
      dsEl.title = "";
    }

    // A team that is fully connected and still bypassed almost certainly should not be.
    // Nothing else on the card says so: the DS pill goes green on link exactly as it would
    // for a station that is going to play, and the bypass checkbox is small and easy to
    // leave ticked from the previous match. Flag it on the one field the operator is
    // reading anyway -- the team number.
    const teamInput = document.getElementById("team-" + station);
    const fullyConnected = Boolean(st.DsConn && st.DsConn.DsLinked && st.DsConn.RobotLinked);
    if (fullyConnected && st.Bypass) {
      teamInput.dataset.bypassWarn = "true";
      teamInput.title = "Bypassed, but the driver station and robot are both connected";
    } else {
      delete teamInput.dataset.bypassWarn;
      teamInput.title = "";
    }

    // E-Stop state — card pulses red, button turns green to show it is active.
    card.dataset.estop = st.EStop ? "true" : "false";
    card.dataset.astop = st.AStop ? "true" : "false";
    estopBtn.dataset.active = st.EStop ? "true" : "false";
    estopBtn.textContent = st.EStop ? "UN-STOP" : "E-STOP";

    // Spell out which stop is active. The card already changes colour, but an operator
    // who does not know the palette cannot read a background alone -- and A-stop's is a
    // muted olive that is easy to miss entirely. E-stop wins when both are latched,
    // matching the card styling.
    const stopEl = document.getElementById("stop-" + station);
    if (st.EStop) {
      stopEl.textContent = "E-STOP";
      stopEl.dataset.kind = "estop";
      stopEl.hidden = false;
    } else if (st.AStop) {
      stopEl.textContent = "A-STOP";
      stopEl.dataset.kind = "astop";
      stopEl.hidden = false;
    } else {
      stopEl.hidden = true;
    }

    // Bypass checkbox.
    bypassChk.checked = st.Bypass;
  }

  // Field e-stop overlay — blocks all controls when hardware e-stop is active.
  document.getElementById("fieldEstopOverlay").style.display =
    data.GpioFieldEStopActive ? "flex" : "none";

  // A wiring fault cannot be cleared by releasing the button, so say which it is.
  const fault = data.GpioFieldEStopFault || "";
  const faultEl = document.getElementById("fieldEstopFault");
  if (faultEl) {
    faultEl.textContent = fault ? "Wiring fault: " + fault : "";
    faultEl.style.display = fault ? "block" : "none";
  }
  const titleEl = document.getElementById("fieldEstopTitle");
  if (titleEl) {
    titleEl.innerHTML = fault
      ? "⛔ FIELD E-STOP WIRING FAULT"
      : "⛔ FIELD E-STOP ACTIVE";
  }

  // Hardware status badges.
  document.getElementById("apStatus").dataset.status = data.AccessPointStatus || "";
  document.getElementById("swStatus").dataset.status = data.SwitchStatus || "";
  // Three states, not two. A field with no e-stop hardware configured reports "not
  // stopped" exactly like a healthy one at rest, so green there would claim a working
  // field e-stop that does not exist. Grey says nothing is watching.
  const estopBadge = document.getElementById("hwEStopStatus");
  const estopStopped = data.GpioFieldEStopActive || data.FieldEStop;
  if (!data.FieldEStopMonitored && !estopStopped) {
    estopBadge.dataset.statusOk = "unmonitored";
    estopBadge.textContent = "FIELD E-STOP N/A";
    estopBadge.title = "No field e-stop configured — see Setup > Settings";
  } else {
    estopBadge.dataset.statusOk = String(!estopStopped);
    estopBadge.textContent = "FIELD E-STOP";
    estopBadge.title = estopStopped ? "Field e-stop active" : "Field e-stop healthy";
  }

  // Update control button states.
  const btnStart = document.getElementById("btnStart");
  const btnAbort = document.getElementById("btnAbort");
  const btnClear = document.getElementById("btnClear");

  switch (matchStates[data.MatchState]) {
    case "PRE_MATCH":
      btnStart.disabled = !data.CanStartMatch;
      btnAbort.disabled = true;
      btnClear.disabled = true;
      break;
    case "START_MATCH":
    case "WARMUP_PERIOD":
    case "AUTO_PERIOD":
    case "PAUSE_PERIOD":
    case "TELEOP_PERIOD":
      btnStart.disabled = true;
      btnAbort.disabled = false;
      btnClear.disabled = true;
      break;
    case "POST_MATCH":
      btnStart.disabled = true;
      btnAbort.disabled = true;
      btnClear.disabled = false;
      break;
    default:
      // An unrecognised state: nothing is safe to offer.
      btnStart.disabled = true;
      btnAbort.disabled = true;
      btnClear.disabled = true;
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

    // Not overwritten while it is being typed into: a match load can land at any moment,
    // and losing a half-entered key to one would be maddening.
    const wpa = document.getElementById("wpaKey-" + station);
    if (document.activeElement !== wpa) {
      wpa.value = team ? team.WpaKey || "" : "";
      // Came from the database, so a team lookup may replace it.
      wpa.dataset.autofilled = "true";
    }
    wpa.disabled = !data.AllowSubstitution;
  }
  document.getElementById("btnRegister").disabled = true;
  document.getElementById("autoWinnerMode").value = data.AutoWinnerMode;
};

const handleMatchTime = function (data) {
  translateMatchTime(data, function (state, stateText, countdown) {
    document.getElementById("periodText").textContent = stateText || "—";
    const secs = Math.max(0, countdown);
    document.getElementById("timerText").textContent =
      state === "POST_MATCH" ? "—" : getCountdownString(secs);

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

// Unlocks the audio elements against the browser's autoplay policy.
//
// Chrome will not play audio until the page has seen a user gesture, and a cue that arrives
// over the websocket does not count as one -- so the first sound of a session is rejected
// even though everything is configured correctly. Playing each element muted inside a real
// gesture satisfies the policy without making a noise, and every later cue is then allowed.
//
// Hooked to the first interaction of any kind rather than to a particular control. Sounds
// are on by default, so there is no one button the operator must press first, and the kiosk
// display may sit untouched for a long time before anyone reaches for it.
let audioPrimed = false;
const primeAudio = function () {
  if (audioPrimed) {
    return;
  }
  audioPrimed = true;

  document.querySelectorAll("audio").forEach(function (element) {
    element.muted = true;
    const played = element.play();
    const reset = function () {
      element.pause();
      element.currentTime = 0;
      element.muted = false;
    };
    if (played === undefined) {
      reset();
    } else {
      played.then(reset).catch(function () {
        element.muted = false;
        audioPrimed = false;
      });
    }
  });
};

// Plays a match cue. Upstream does this on the audience display; this fork has none, so the
// operator's own page is the only thing that can make a noise.
//
// Any cue still playing is stopped first: the sounds are close together at the end of a
// match, and two overlapping is worse than one cut short.
const handlePlaySound = function (sound) {
  document.querySelectorAll("audio").forEach(function (element) {
    element.pause();
    element.currentTime = 0;
  });

  const element = document.getElementById("sound-" + sound);
  if (element === null) {
    // A cue with no element is not worth breaking the page over -- the match is running.
    console.warn("No audio element for sound '" + sound + "'.");
    return;
  }

  // Browsers refuse to play until the page has been interacted with, and reject the promise
  // rather than throwing. Unhandled, that surfaces as a console error on every cue.
  const played = element.play();
  if (played !== undefined) {
    played.catch(function (error) {
      console.warn("Could not play '" + sound + "': " + error);
    });
  }
};

$(function () {
  websocket = new CheesyWebsocket("/match_play/websocket", {
    arenaStatus: function (event) { handleArenaStatus(event.data); },
    matchLoad:   function (event) { handleMatchLoad(event.data); },
    matchTime:   function (event) { handleMatchTime(event.data); },
    matchTiming: function (event) { handleMatchTiming(event.data); },
    playSound:   function (event) { handlePlaySound(event.data); },
  });

  // Any interaction will do. Registered on the capture phase so a click on a control primes
  // before that control's own handler runs -- pressing Start Match should not lose the start
  // cue to the gesture that caused it.
  //
  // Deliberately not {once: true}: priming can fail, and that removes the listener whether
  // it worked or not, leaving the page permanently unable to retry. primeAudio guards
  // against repeats itself and clears its own flag on failure, so a later gesture tries
  // again.
  ["pointerdown", "keydown", "touchstart"].forEach(function (event) {
    document.addEventListener(event, primeAudio, true);
  });
});

if (typeof module !== "undefined") {
  module.exports = { handleArenaStatus, handleMatchLoad, handleMatchTime, registerTeams, clearMatch };
}
