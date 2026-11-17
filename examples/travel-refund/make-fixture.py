"""Writes a made-up booking to try the travel refund example on.

usage: python3 make-fixture.py DIR

Three text records of an invented domestic flight that the airline
cancelled. The airline, the passenger, the reference and the ticket
number are invented. Nothing says how the refund should be paid out or
whether one was asked for: those are left for the agent to call unknown.
"""
import os
import sys

root = sys.argv[1]
os.makedirs(root, exist_ok=True)
records = {
    "01-booking-confirmation.txt": """Example Air booking confirmation
Booking reference: EXA7Q2
Passenger: Jordan Example
Ticket number: 000-0000000001
Flight: EX 418
From: Springfield (SPI)  To: Portland (PDX)
Departure: October 14, 2026 at 8:05 AM
Fare: $371.16  Taxes and fees: $41.44  Total paid: $412.60
Paid on: September 2, 2026 with a credit card ending 0000
Fare type: non-refundable
""",
    "02-cancellation-notice.txt": """From: Example Air <no-reply@exampleair.example>
Date: October 13, 2026
Subject: Your flight EX 418 has been cancelled

We are sorry. Flight EX 418 on October 14, 2026 has been cancelled by
Example Air. We have rebooked you on EX 422 departing October 15, 2026 at
6:40 PM. You can accept this flight or decline it in Manage Booking.
Booking reference: EXA7Q2
""",
    "03-my-notes.txt": """notes
- declined the new flight in the app on Oct 13, did not travel
- drove instead
- have not heard anything about money back
""",
}
for name, text in records.items():
    with open(os.path.join(root, name), "w") as f:
        f.write(text)
print("wrote", len(records), "records under", root)
