using System;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using System.Threading.Tasks;

namespace Paymetheus.Decred.Wallet
{
    public sealed class StakeInfoProperties
    {
        public uint PoolSize { get; set; }
        public uint AllMempoolTickets { get; set; }
        public uint OwnMempoolTickets { get; set; }
        public uint Immature { get; set; }
        public uint Live { get; set; }
        public uint Voted { get; set; }
        public uint Missed { get; set; }
        public uint Revoked { get; set; }
        public uint Expired { get; set; }

        public Amount TotalSubsidy { get; set; }
    }
}
