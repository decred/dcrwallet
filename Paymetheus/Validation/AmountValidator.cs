using Paymetheus.Decred;
using System;
using System.Collections.Generic;
using System.Globalization;
using System.Linq;
using System.Text;
using System.Threading.Tasks;
using System.Windows.Controls;

namespace Paymetheus.Validation
{
    class AmountValidator : ValidationRule
    {
        public bool AllowNegative { get; set; } = false;

        public bool RestrictProducable { get; set; } = true;

        public override ValidationResult Validate(object value, CultureInfo cultureInfo)
        {
            try
            {
                double doubleResult;
                if (!double.TryParse((string)value, out doubleResult))
                {
                    return new ValidationResult(false, "String to double conversion failed");
                }

                var amount = Denomination.Decred.AmountFromDouble(doubleResult);

                if (!AllowNegative && amount < 0)
                {
                    return new ValidationResult(false, "Value must be non-negative");
                }

                if (RestrictProducable)
                {
                    var positiveAmount = amount;
                    if (positiveAmount < 0)
                    {
                        positiveAmount = -amount;
                    }
                    if (!TransactionRules.IsSaneOutputValue(positiveAmount))
                    {
                        return new ValidationResult(false, "Value exceeds allowed bounds");
                    }
                }

                return new ValidationResult(true, null);
            }
            catch (Exception e)
            {
                return new ValidationResult(false, e);
            }
        }
    }
}
