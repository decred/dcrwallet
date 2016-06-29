using System;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using System.Threading.Tasks;

namespace Paymetheus.DTO
{
    public class RecentActivity
    {
        public bool IsDeposit { get; set; }
        public string WalletName { get; set; }
        public DateTime TransactionLocalTime { get; set; }
        public TransactionUserControl.TransactionStatus Status { get; set; }
        public double Ammount { get; set; }
        public string OperatingTo { get; set; }
        public int Confirmations { get; set; }
    }
}
