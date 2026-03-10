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
  // Clear inputs immediately so clicking Clear always resets the fields,
  // even if the slot was never registered (so arena status never fires a clear).
  $("#teamId-" + station).val("").data("arenaSet", false);
  $("#wpaKey-" + station).val("").data("arenaSet", false);
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
  module.exports = { handleArenaStatus };
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
