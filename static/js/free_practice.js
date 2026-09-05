// Copyright 2014 Team 254. All Rights Reserved.
//
// Client-side logic for the free practice operator page.

var websocket;

// Tracks whether the field is already in free practice, so that ENABLE FIELD can mean
// "make the field live" in both places it appears. The server rejects enterFreePractice
// outside PreMatch, so the choice has to be made here.
let inFreePracticeMode = false;

// -- WebSocket command senders --

const enterFreePractice = function () {
  websocket.send("enterFreePractice");
};

// ENABLE FIELD: enters free practice from setup, or resumes robot operation on a field
// the operator has halted.
const enableField = function () {
  websocket.send(inFreePracticeMode ? "enableField" : "enterFreePractice");
};

// DISABLE FIELD: halts robot operation. Teams stay registered, SSIDs and team subnets
// stay up, driver stations stay connected.
const disableField = function () {
  websocket.send("disableField");
};

// Reset Field: clears every slot, drops all SSIDs and team subnets, returns to setup.
const exitFreePractice = function () {
  websocket.send("exitFreePractice");
};

const setSlot = function (station) {
  const teamId = parseInt($("#teamId-" + station).val(), 10);
  const wpaKey = $("#wpaKey-" + station).val().trim();
  if (!teamId || teamId < 1) {
    alert("Team number must be 1 or greater.");
    return;
  }
  websocket.send("setSlot", {Station: station, TeamId: teamId, WpaKey: wpaKey});
};

const clearSlot = function (station) {
  // Clear inputs immediately so clicking Clear always resets the fields,
  // even if the slot was never registered (so arena status never fires a clear).
  $("#teamId-" + station).val("").data("arenaSet", false);
  $("#wpaKey-" + station).val("").data("arenaSet", false);
  websocket.send("clearSlot", station);
};

const toggleEStop = function (station) {
  websocket.send("toggleEStop", station);
};

const clearFieldEStop = function () {
  websocket.send("clearFieldEStop", null);
};

// -- Arena status handler --

// Set once a navigation is under way, so the broadcasts that arrive before the page
// unloads do not each trigger another one.
let leavingForOtherView = false;

const handleArenaStatus = function (data) {
  // Every kiosk shows the same operating page -- see the matching block in match_play.js.
  // Free practice is set up while the arena is still in PreMatch, so this follows the
  // recorded view rather than the match state, which would eject the operator mid-setup.
  if (data.CurrentView && data.CurrentView !== "free_practice" && !leavingForOtherView) {
    leavingForOtherView = true;
    window.location.href = "/" + data.CurrentView;
    return;
  }

  // FreePracticeState is injected as a JS constant by the HTML template.
  const inFreePractice = data.MatchState === FreePracticeState;
  const halted = Boolean(data.FieldDisabled);
  inFreePracticeMode = inFreePractice;

  // Update the mode banner. A halted field is not the live green state -- robots are not
  // drivable -- but it is not setup either, so it says so.
  const live = inFreePractice && !halted;
  const banner = $("#fpModeBanner");
  banner.toggleClass("fp-mode-setup", !live).toggleClass("fp-mode-enabled", live);
  let label = "FREE PRACTICE SETUP";
  if (inFreePractice) {
    label = halted ? "FREE PRACTICE — FIELD DISABLED" : "FREE PRACTICE ENABLED";
  }
  $("#fpModeLabel").text(label);

  // ENABLE FIELD appears in setup and on a halted field; DISABLE FIELD only when robots
  // are live. Reset Field stays available throughout free practice.
  $("#enterBtn").toggleClass("d-none", live);
  $("#disableBtn").toggleClass("d-none", !live);
  $("#resetBtn").toggleClass("d-none", !inFreePractice);

  // Disable the Match Play link while free practice is running.
  // The server also redirects /match_play → /free_practice?warn=1, so this
  // is defence-in-depth on the client side.
  $("#matchPlayBtn")
    .toggleClass("disabled", inFreePractice)
    .attr("aria-disabled", inFreePractice ? "true" : "false");

  // Reconfiguring overlay.
  $("#reconfiguringOverlay").toggleClass("d-none", !data.FreePracticeReconfiguring);

  // Field e-stop overlay (mirrors match_play behavior).
  document.getElementById("fieldEstopOverlay").style.display =
    data.GpioFieldEStopActive ? "flex" : "none";

  // Hardware status badges.
  document.getElementById("apStatus").dataset.status = data.AccessPointStatus || "";
  document.getElementById("swStatus").dataset.status = data.SwitchStatus || "";
  document.getElementById("hwEStopStatus").dataset.statusOk =
    String(!(data.GpioFieldEStopActive || data.FieldEStop));

  const stations = ["R1", "R2", "R3", "B1", "B2", "B3"];
  stations.forEach(function (s) {
    const as = data.AllianceStations[s];
    const statusEl = $("#status-" + s);
    const slotCard = $("#slot-" + s);

    // DS connection status text.
    let statusText = "Empty slot";
    if (as && as.Team && as.Team.Id) {
      statusText = "Team " + as.Team.Id;
      if (as.DsConn) {
        if (as.DsConn.RobotLinked) {
          statusText += " — Robot linked";
        } else {
          statusText += " — DS connected";
        }
      } else {
        statusText += " — No DS";
      }
      if (as.EStop) {
        statusText += " [E-STOP]";
        slotCard.addClass("border-danger");
      } else {
        slotCard.removeClass("border-danger");
      }
      // Populate the input fields so the operator can see the current registration.
      if (!$("#teamId-" + s).is(":focus")) {
        $("#teamId-" + s).val(as.Team.Id).data("arenaSet", true);
      }
      if (!$("#wpaKey-" + s).is(":focus")) {
        $("#wpaKey-" + s).val(as.Team.WpaKey || "").data("arenaSet", true);
      }
    } else {
      slotCard.removeClass("border-danger");
      // Only clear inputs that arena status itself previously wrote.
      // If the operator has typed a value that hasn't been registered yet,
      // leave it alone so it isn't wiped by the next status push.
      if (!$("#teamId-" + s).is(":focus")) {
        if ($("#teamId-" + s).data("arenaSet") || !$("#teamId-" + s).val()) {
          $("#teamId-" + s).val("").data("arenaSet", false);
        }
      }
      if (!$("#wpaKey-" + s).is(":focus")) {
        if ($("#wpaKey-" + s).data("arenaSet") || !$("#wpaKey-" + s).val()) {
          $("#wpaKey-" + s).val("").data("arenaSet", false);
        }
      }
    }
    // Cabling mismatches, which nothing downstream can catch: a laptop in the wrong
    // station never gets an address, so it never connects, so no wrong-station check ever
    // runs. Only reported while the switch is actually telling us about link.
    if (data.StationLinksKnown) {
      const registered = Boolean(as && as.Team && as.Team.Id);
      if (registered && !as.PortLinked) {
        statusText += " — nothing plugged into this port";
      } else if (!registered && as && as.PortLinked) {
        statusText += " — cable connected, no team registered";
      }
    }

    statusEl.text(statusText);

    // Disable inputs while reconfiguring (allowed in both setup and enabled states).
    const disabled = data.FreePracticeReconfiguring;
    slotCard.find("input, button:not(.btn-danger)").prop("disabled", disabled);
    slotCard.find(".btn-danger").prop("disabled", !inFreePractice);
  });
};

// -- Page init --

$(function () {
  websocket = new CheesyWebsocket("/free_practice/websocket", {
    arenaStatus: function (event) {
      handleArenaStatus(event.data);
    },
  });

  // Prevent navigation via the Match Play button when it is disabled.
  $(document).on("click", "#matchPlayBtn.disabled", function (e) {
    e.preventDefault();
  });

  // Auto-populate the WPA key when a team number is entered.
  // If the team is not in the DB, prompt the operator to add it.
  $(document).on("blur", "[id^='teamId-']", function () {
    const station = this.id.replace("teamId-", "");
    const teamId = parseInt($(this).val(), 10);
    if (!teamId || teamId < 1) return;
    fetch("/setup/teams/" + teamId)
      .then(r => {
        if (r.ok) {
          return r.json().then(data => {
            if (data.wpaKey) {
              $("#wpaKey-" + station).val(data.wpaKey);
            }
          });
        }
        if (r.status === 404) {
          showTeamNotInDbModal(teamId, station);
        }
      })
      .catch(() => {});
  });
});

// Export for Jest unit tests. No-op in the browser (module is undefined).
if (typeof module !== "undefined") {
  module.exports = {
    handleArenaStatus,
    clearFieldEStop,
    enableField,
    disableField,
    exitFreePractice,
    // The page assigns the websocket in its ready handler, which does not run under
    // jsdom. Tests that exercise a command sender supply their own here.
    setWebsocket: function (ws) {
      websocket = ws;
    },
  };
}

// -- Team-not-in-DB modal --

function showTeamNotInDbModal(teamId, station) {
  $("#teamNotInDbMessage").text(
    "Team " + teamId + " is not in the database. Exit free practice to add it via Setup → Teams, or add it now."
  );

  const modalEl = document.getElementById("teamNotInDbModal");
  const modal = bootstrap.Modal.getOrCreateInstance(modalEl);

  // Re-bind buttons each time to capture current teamId/station.
  $("#teamNotInDbCancel").off("click").on("click", function () {
    $("#teamId-" + station).val("");
    modal.hide();
  });

  $("#teamNotInDbAdd").off("click").on("click", function () {
    fetch("/setup/teams/quick-add", {
      method: "POST",
      headers: {"Content-Type": "application/x-www-form-urlencoded"},
      body: "id=" + teamId,
    })
      .then(r => {
        if (r.ok) {
          modal.hide();
        } else {
          r.text().then(msg => alert("Failed to add team: " + msg));
        }
      })
      .catch(() => alert("Failed to add team."));
  });

  modal.show();
}
