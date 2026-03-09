// Copyright 2014 Team 254. All Rights Reserved.
//
// Client-side logic for the free practice operator page.

var websocket;

// -- WebSocket command senders --

const enterFreePractice = function () {
  websocket.send("enterFreePractice");
};

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
  websocket.send("clearSlot", station);
};

const toggleEStop = function (station) {
  websocket.send("toggleEStop", station);
};

// -- Arena status handler --

const handleArenaStatus = function (data) {
  // FreePracticeState is injected as a JS constant by the HTML template.
  const inFreePractice = data.MatchState === FreePracticeState;

  // Toggle UI sections based on current state.
  $("#enterBtn").toggleClass("d-none", inFreePractice);
  $("#exitBtn").toggleClass("d-none", !inFreePractice);

  // Reconfiguring overlay.
  $("#reconfiguringOverlay").toggleClass("d-none", !data.FreePracticeReconfiguring);

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
        $("#teamId-" + s).val(as.Team.Id);
      }
    } else {
      slotCard.removeClass("border-danger");
      if (!$("#teamId-" + s).is(":focus")) {
        $("#teamId-" + s).val("");
      }
    }
    statusEl.text(statusText);

    // Disable inputs when not in free practice.
    const disabled = !inFreePractice || data.FreePracticeReconfiguring;
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
});
