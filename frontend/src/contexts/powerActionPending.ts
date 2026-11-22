// "A reboot or power-off is under way". A host going down ends the session
// before the webserver exits, so the stream reports the session as expired;
// AuthProvider reads this to leave that exit to the power overlay.

let pending = false;

export const isPowerActionPending = () => pending;

export const setPowerActionPending = (value: boolean) => {
  pending = value;
};
