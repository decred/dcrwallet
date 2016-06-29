using Paymetheus.Framework.ValueConverters;
using Paymetheus.ViewModels;
using System;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using System.Threading.Tasks;

namespace Paymetheus
{
    class AccountToNameConverter : OneWayConverter<AccountViewModel, string>
    {
        public AccountToNameConverter() : base(a => a.AccountProperties.AccountName) { }
    }
}
