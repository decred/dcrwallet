using System;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using System.Threading.Tasks;

namespace Paymetheus.Decred.Wallet
{
    public sealed class StakeDifficultyProperties
    {
        public int HeightForTicketPrice { get; set; }
        public Amount NextTicketPrice { get; set; }
    }
}
