using System;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using System.Threading.Tasks;

namespace Paymetheus.Decred.Wallet
{
    public enum TransactionCategory
    {
        Send,
        Receive,
        TicketPurchase,
        TicketRevocation,
        Vote,
        Unknown,
    }
}
